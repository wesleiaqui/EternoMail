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

func (r *SyncRuntime) enqueueFilter(rule *mailfilter.Rule, msg *message.Message) {
	if r == nil || rule == nil || msg == nil {
		return
	}
	if !rule.HasActions() {
		return
	}
	if err := r.filterJobs.Enqueue(rule, rule.DestFolderID, msg); err != nil {
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
	pauseHidden := func() {
		if err := r.filterJobs.DeferUnstarted(job.ID); err != nil {
			log.Error().Err(err).Str("jobID", job.ID).Msg("Could not pause hidden-destination filter job")
		}
	}
	rule, err := r.rules.Get(job.RuleID)
	if err != nil {
		r.markFilterJobUnknown(job, err)
		return
	}
	if rule == nil || (!rule.Enabled && !job.Actions.ApplyExisting) || rule.DestFolderID != job.DestinationFolderID || rule.ActionMarkRead != job.Actions.MarkRead || rule.ActionArchive != job.Actions.Archive || rule.ActionTrash != job.Actions.Trash {
		finishStale("rule changed")
		return
	}
	var destination *folder.Folder
	if job.DestinationFolderID != "" {
		destination, err = r.folders.Get(job.DestinationFolderID)
		if err != nil {
			r.markFilterJobUnknown(job, err)
			return
		}
		if destination != nil && destination.Hidden {
			pauseHidden()
			return
		}
		if destination == nil {
			r.markFilterJobUnknown(job, errMissingFilterDestination)
			return
		}
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
	defer cancel()

	// Recheck criteria and current source before starting any provider actions.
	current[0].ListID, err = r.messages.GetListID(current[0].ID)
	if err != nil {
		r.markFilterJobUnknown(job, err)
		return
	}
	effective := *rule
	effective.Enabled = rule.Enabled || job.Actions.ApplyExisting
	if current[0].IsDeleted || current[0].IsDraft || !mailfilter.Match(&effective, mailfilter.FromMessage(current[0])) {
		finishStale("criteria changed")
		return
	}
	ops := &mailboxActionOperations{runtime: r, contextOverride: jobContext}
	if job.Actions.Archive || job.Actions.Trash {
		kind := folder.TypeTrash
		if job.Actions.Archive {
			kind = folder.TypeArchive
			_, isGmail, err := ops.gmailClient(job.SourceAccountID)
			if err != nil {
				r.markFilterJobUnknown(job, err)
				return
			}
			if isGmail {
				kind = folder.TypeAll
			}
		}
		destination, err = r.folders.GetByType(job.SourceAccountID, kind)
		if err != nil {
			r.markFilterJobUnknown(job, err)
			return
		}
		if destination == nil {
			r.markFilterJobUnknown(job, errMissingFilterDestination)
			return
		}
		if destination.Hidden {
			pauseHidden()
			return
		}
	}
	if job.Actions.MarkRead {
		if err := ops.MarkAsRead([]string{job.MessageID}); err != nil {
			r.markFilterJobUnknown(job, err)
			return
		}
	}
	switch {
	case destination != nil:
		effective.DestFolderID = destination.ID
		applied, err := r.filterOp.Apply(jobContext, &effective, destination, current[0])
		if err != nil {
			r.markFilterJobUnknown(job, err)
			return
		}
		if !applied {
			finishStale("source changed")
			return
		}

	}

	if err := r.filterJobs.Complete(job.ID); err != nil {
		log.Error().Err(err).Str("jobID", job.ID).Msg("Filter actions ran but automatic filter completion was not persisted; outcome needs inspection")
		return
	}
	log.Info().Str("ruleID", rule.ID).Msg("Automatic filter applied to message")
}

func (r *SyncRuntime) markFilterJobUnknown(job *mailfilter.FilterJob, cause error) {
	log := logging.WithComponent("alwayson.filters")
	if err := r.filterJobs.MarkOutcomeUnknown(job.ID, cause.Error()); err != nil {
		log.Error().Err(err).Str("jobID", job.ID).Msg("Could not persist uncertain automatic filter outcome")
		return
	}
	log.Warn().Err(cause).Str("ruleID", job.RuleID).Str("jobID", job.ID).Msg("Automatic filter actions stopped; outcome needs inspection and will not be retried automatically")
}
