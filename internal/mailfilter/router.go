package mailfilter

import (
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
)

// EnabledRuleLister reads the persisted routing rules used by the automatic
// incoming-message path.
type EnabledRuleLister interface {
	ListEnabled() ([]*Rule, error)
}

// FolderGetter resolves the configured destination for a rule.
type FolderGetter interface {
	Get(id string) (*folder.Folder, error)
}

// RuleApplier performs the actual routing operation. The caller supplies the
// shared safe-move implementation so desktop and headless service adapters do
// not grow separate move behavior.
type RuleApplier func(rule *Rule, destination *folder.Folder, msg *message.Message)

// AutomaticRouter chooses the first matching enabled rule for a newly synced
// message. It intentionally delegates mutations to the caller's shared move
// operation; this type owns rule lookup and routing decisions only.
type AutomaticRouter struct {
	rules   EnabledRuleLister
	folders FolderGetter
	apply   RuleApplier
}

func NewAutomaticRouter(rules EnabledRuleLister, folders FolderGetter, apply RuleApplier) *AutomaticRouter {
	return &AutomaticRouter{rules: rules, folders: folders, apply: apply}
}

// Route evaluates enabled rules in priority order and applies the first match.
// It is safe to call from a sync callback; implementations can enqueue the
// potentially slow move through RuleApplier.
func (r *AutomaticRouter) Route(msg *message.Message) {
	if r == nil || msg == nil || r.rules == nil || r.folders == nil || r.apply == nil {
		return
	}
	log := logging.WithComponent("mailfilter.router")
	rules, err := r.rules.ListEnabled()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load enabled filter rules")
		return
	}
	criteria := FromMessage(msg)
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		destination, err := r.folders.Get(rule.DestFolderID)
		if err != nil {
			log.Warn().Err(err).Str("ruleID", rule.ID).Msg("Filter rule destination lookup failed")
			continue
		}
		if destination == nil || destination.Hidden {
			// Missing destinations need repair. Hidden destinations pause dynamically
			// without changing the persisted Enabled choice.
			log.Debug().Str("ruleID", rule.ID).Str("destFolderID", rule.DestFolderID).Msg("Filter rule destination missing or hidden, skipping rule")
			continue
		}
		if !Match(rule, criteria, destination.AccountID) {
			continue
		}
		r.apply(rule, destination, msg)
		return
	}
}
