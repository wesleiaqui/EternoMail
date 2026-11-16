package nativemenu

import (
	"reflect"
	"testing"
)

func TestCatalogAndDisabledStartup(t *testing.T) {
	want := []string{"File", "Edit", "View", "Message", "Mailbox", "Format", "Go", "Window", "Help"}
	var got []string
	c := New()
	for _, item := range Catalog() {
		got = append(got, item.Label)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("menu order: %v", got)
	}
	for id, items := range c.items {
		if id == "" {
			t.Fatal("empty action id")
		}
		for _, item := range items {
			if !item.Disabled {
				t.Fatalf("startup action %s enabled before frontend readiness", id)
			}
		}
	}
	if len(c.items["calendar.new"]) != 2 {
		t.Fatal("shared New Event entry missing")
	}
	// Preserve only the four native edit accelerators. Other existing bindings
	// stay with their current context-aware webview handlers.
	for id, items := range c.items {
		for _, item := range items {
			edit := id == "edit.cut" || id == "edit.copy" || id == "edit.paste" || id == "edit.selectAll"
			if (item.Accelerator != nil) != edit {
				t.Fatalf("unexpected accelerator for %s", id)
			}
		}
	}
}
func TestStateReconciliationAcrossSelectionAndPageReload(t *testing.T) {
	c := New()
	if !c.applyState(State{Epoch: 100, Enabled: []string{"calendar.new", "composer.send", "not-a-catalog-id"}}) {
		t.Fatal("first enable did not refresh")
	}
	for _, item := range c.items["calendar.new"] {
		if item.Disabled {
			t.Fatal("duplicate entry did not enable")
		}
	}
	if c.applyState(State{Epoch: 100, Enabled: []string{"calendar.new", "composer.send"}}) {
		t.Fatal("identical state needlessly refreshed")
	}
	if c.applyState(State{Epoch: 99, Enabled: []string{"mail.trash"}}) {
		t.Fatal("stale state refreshed menu")
	}
	if !c.items["mail.trash"][0].Disabled {
		t.Fatal("stale state enabled destructive action")
	}
	if !c.applyState(State{Epoch: 200, Enabled: []string{"mail.reply"}}) {
		t.Fatal("new page state did not refresh")
	}
	if !c.items["composer.send"][0].Disabled {
		t.Fatal("old composer Send remained enabled")
	}
	for _, item := range c.items["calendar.new"] {
		if !item.Disabled {
			t.Fatal("old duplicate entry remained enabled")
		}
	}
	if c.applyState(State{Epoch: 101, Enabled: []string{"composer.send"}}) {
		t.Fatal("delayed old page update accepted")
	}
	if !c.items["composer.send"][0].Disabled {
		t.Fatal("delayed old page enabled Send")
	}
}

func TestDetachedComposerKeepsCatalogEditAndDraftHistory(t *testing.T) {
	for _, c := range []*Controller{New(), NewComposer()} {
		for _, id := range []string{"edit.cut", "edit.copy", "edit.paste", "edit.selectAll", "edit.undo", "composer.redo", "composer.history"} {
			if len(c.items[id]) == 0 {
				t.Fatalf("Edit command missing: %s", id)
			}
		}
	}
}

func TestContextualTaskMenuMatchesSelectedView(t *testing.T) {
	c := New()
	for i, tc := range []struct{ view, label, first string }{{"mail", "Message", "Reply"}, {"calendar", "Event", "New Event…"}, {"contacts", "Contact", "New Contact…"}, {"unknown", "Message", "Reply"}} {
		c.applyState(State{Epoch: uint64(i + 1), View: tc.view})
		if c.taskMenu.Label != tc.label {
			t.Fatalf("view %s got %s", tc.view, c.taskMenu.Label)
		}
		if c.taskMenu.SubMenu != c.taskViews[map[string]string{"Message": "mail", "Event": "calendar", "Contact": "contacts"}[tc.label]] {
			t.Fatal("incorrect task submenu")
		}
	}
	c.applyState(State{Epoch: 100, View: "calendar"})
	c.applyState(State{Epoch: 99, View: "contacts"})
	if c.taskMenu.Label != "Event" {
		t.Fatal("stale view changed task menu")
	}
}
