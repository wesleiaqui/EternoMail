package alwayson

import (
	"context"
	"errors"
	"time"

	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/mailfilter"
	"github.com/hkdb/aerion/internal/message"
)

var errMissingFilterDestination = errors.New("automatic filter destination is missing")

func (r *SyncRuntime) enqueueFilter(rule *mailfilter.Rule, destination *folder.Folder, msg *message.Message) {
	if r == nil || rule == nil || destination == nil || msg == nil {
		return
	}
	if err := r.filterJobs.Enqueue(rule, destination.ID, msg); err != nil {
		log := logging.WithComponent("alwayson.filters")
		log.Error().Err(err).Str("ruleID", rule.ID).Str("messageID", msg.ID).Msg("Failed to persist automatic filter job")
		return
	}
	select {
	case r.filterWake <- struct{}{}:
	default:
	}
}

func (r *SyncRuntime) runFilterQueue(ctx context.Context) {
	defer r.filterWG.Done()
	log := logging.WithComponent("alwayson.filters")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		job, err := r.filterJobs.ClaimNext()
		if err != nil {
			log.Error().Err(err).Msg("Failed to claim durable automatic filter job")
		} else if job != nil {
			r.runFilterJob(ctx, job)
			continue
		}
		select {
		case _, open := <-r.filterWake:
			if !open {
				return
			}
		case <-r.filterStop:
			return
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (r *SyncRuntime) runFilterJob(ctx context.Context, job *mailfilter.FilterJob) {
	log := logging.WithComponent("alwayson.filters")
	finishStale := func(reason string) {
		if err := r.filterJobs.Complete(job.ID); err != nil {
			log.Error().Err(err).Str("jobID", job.ID).Str("reason", reason).Msg("Could not remove stale automatic filter job")
		}
	}
	rule, err := r.rules.Get(job.RuleID)
	if err != nil {
		r.markFilterJobUnknown(job, err)
		return
	}
	if rule == nil || !rule.Enabled || rule.DestFolderID != job.DestinationFolderID {
		finishStale("rule changed")
		return
	}
	destination, err := r.folders.Get(job.DestinationFolderID)
	if err != nil {
		r.markFilterJobUnknown(job, err)
		return
	}
	if destination == nil {
		r.markFilterJobUnknown(job, errMissingFilterDestination)
		return
	}
	if destination.Hidden {
		if err := r.filterJobs.DeferUnstarted(job.ID); err != nil {
			log.Error().Err(err).Str("jobID", job.ID).Msg("Could not pause hidden-destination filter job")
		}
		return
	}
	current, err := r.filterOp.Messages.GetByIDs([]string{job.MessageID})
	if err != nil {
		r.markFilterJobUnknown(job, err)
		return
	}
	if len(current) != 1 || current[0] == nil || current[0].AccountID != job.SourceAccountID || current[0].FolderID != job.SourceFolderID || current[0].UID != job.SourceUID {
		finishStale("source changed")
		return
	}
	jobContext, cancel := context.WithTimeout(ctx, 45*time.Second)
	applied, err := r.filterOp.Apply(jobContext, rule, destination, current[0])
	cancel()
	if err != nil {
		r.markFilterJobUnknown(job, err)
		return
	}
	if !applied {
		finishStale("source no longer available")
		return
	}
	if err := r.filterJobs.Complete(job.ID); err != nil {
		log.Error().Err(err).Str("jobID", job.ID).Msg("Move succeeded but automatic filter completion was not persisted; outcome needs inspection")
		return
	}
	log.Info().Str("ruleID", rule.ID).Str("destinationFolder", destination.Path).Msg("Automatic filter routed message")
}

func (r *SyncRuntime) markFilterJobUnknown(job *mailfilter.FilterJob, cause error) {
	log := logging.WithComponent("alwayson.filters")
	if err := r.filterJobs.MarkOutcomeUnknown(job.ID, cause.Error()); err != nil {
		log.Error().Err(err).Str("jobID", job.ID).Msg("Could not persist uncertain automatic filter outcome")
		return
	}
	log.Warn().Err(cause).Str("ruleID", job.RuleID).Str("jobID", job.ID).Msg("Automatic filter move stopped; outcome needs inspection and will not be retried automatically")
}
