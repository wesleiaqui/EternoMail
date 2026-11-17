package mailfilter

import (
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/message"
	"testing"
)

type testRoutingRules struct{ rules []*Rule }

func (r testRoutingRules) ListEnabled() ([]*Rule, error) { return r.rules, nil }

type testRoutingFolders struct{ dest *folder.Folder }

func (f testRoutingFolders) Get(string) (*folder.Folder, error) { return f.dest, nil }
func TestHiddenDestinationPausesWithoutRewritingIntent(t *testing.T) {
	rule := &Rule{ID: "rule", Name: "Archive", Enabled: true, DestFolderID: "dest"}
	dest := &folder.Folder{ID: "dest", AccountID: "b", Hidden: true}
	calls := 0
	router := NewAutomaticRouter(testRoutingRules{[]*Rule{rule}}, testRoutingFolders{dest}, func(*Rule, *folder.Folder, *message.Message) { calls++ })
	msg := &message.Message{ID: "m", AccountID: "a", FolderID: "inbox"}
	router.Route(msg)
	if calls != 0 || !rule.Enabled {
		t.Fatal("hidden destination routed or rewrote intent")
	}
	dest.Hidden = false
	router.Route(msg)
	if calls != 1 || !rule.Enabled {
		t.Fatal("show did not resume existing rule")
	}
}
