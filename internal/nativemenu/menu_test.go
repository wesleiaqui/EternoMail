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

func TestNativeHintCatalogCoverageAndKeyboardOwnership(t *testing.T) {
	c := New()
	var check func([]Item)
	check = func(definitions []Item) {
		for _, def := range definitions {
			check(def.Children)
			if def.Key == "" {
				continue
			}
			key, _ := shortcutHint(def.Key)
			if key == "" {
				t.Fatalf("unhandled catalog shortcut %s: %q", def.ID, def.Key)
			}
			for _, item := range c.items[def.ID] {
				if c.hints[item] != def.Key {
					t.Fatalf("missing native hint for %s", def.ID)
				}
				if item.Accelerator != nil && def.ID != "edit.cut" && def.ID != "edit.copy" && def.ID != "edit.paste" && def.ID != "edit.selectAll" {
					t.Fatalf("hint stole frontend binding %s", def.ID)
				}
			}
		}
	}
	check(Catalog())
	for epoch, view := range []string{"mail", "calendar", "contacts"} {
		c.applyState(State{Epoch: uint64(epoch + 1), View: view})
		tree := c.hintTree(c.menu.Items)
		if len(tree) == 0 {
			t.Fatal("missing native tree")
		}
		var count int
		var walk func([]menuHint)
		walk = func(hints []menuHint) {
			for _, hint := range hints {
				if hint.Key != "" {
					count++
				}
				walk(hint.Children)
			}
		}
		walk(tree)
		if count < 40 {
			t.Fatalf("view %s lost shortcut hints: %d", view, count)
		}
	}
}

func TestShortcutHintSpecialKeysAndModifiers(t *testing.T) {
	for _, tc := range []struct {
		glyph, key string
		mod        uint
	}{
		{"⌘⇧R", "r", hintCommand | hintShift},
		{"⌘⇧⌥L", "l", hintCommand | hintShift | hintOption},
		{"⇧I", "i", hintShift}, {"H", "h", 0},
		{"⌘↩", "\r", hintCommand}, {"⌃⇥", "\t", hintControl},
		{"⇧⌫", "\x7f", hintShift}, {"⇧⌦", "\uf728", hintShift}, {"Esc", "\x1b", 0}, {"Space", " ", 0},
		{"⌥↓", "\uf701", hintOption}, {"⌘←", "\uf702", hintCommand},
		{"unknown", "", 0}, {"", "", 0},
	} {
		key, mod := shortcutHint(tc.glyph)
		if key != tc.key || mod != tc.mod {
			t.Errorf("%q -> %q/%d, want %q/%d", tc.glyph, key, mod, tc.key, tc.mod)
		}
	}
}

func TestCatalogLabelsAreUnambiguousWithinEachMenu(t *testing.T) {
	var walk func([]Item)
	walk = func(items []Item) {
		labels := map[string]bool{}
		for _, item := range items {
			if item.Label != "-" && labels[item.Label] {
				t.Fatalf("ambiguous menu label %q", item.Label)
			}
			labels[item.Label] = true
			walk(item.Children)
		}
	}
	walk(Catalog())
}
