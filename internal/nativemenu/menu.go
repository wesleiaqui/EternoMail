// Package nativemenu builds native menus from the same catalog used by the UI.
package nativemenu

import (
	"context"
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed catalog.json
var catalogJSON []byte

type Item struct {
	Label    string `json:"label"`
	ID       string `json:"id"`
	Key      string `json:"key"`
	Children []Item `json:"children"`
}

// Catalog returns the shared ordered native-menu definitions.
func Catalog() []Item {
	var items []Item
	if err := json.Unmarshal(catalogJSON, &items); err != nil {
		panic(err)
	}
	return items
}

type State struct {
	View    string   `json:"view"`
	Epoch   uint64   `json:"epoch"`
	Enabled []string `json:"enabled"`
}
type Controller struct {
	mu        sync.Mutex
	ctx       context.Context
	items     map[string][]*menu.MenuItem
	epoch     uint64
	menu      *menu.Menu
	taskMenu  *menu.MenuItem
	taskViews map[string]*menu.Menu
}

// New creates disabled native menus until a frontend publishes valid state.
func New() *Controller { return newController(false) }

// NewComposer preserves AppKit clipboard formats without replacing draft Undo.
func NewComposer() *Controller { return newController(true) }

func newController(nativeEdit bool) *Controller {
	c := &Controller{items: map[string][]*menu.MenuItem{}}
	var build func([]Item) *menu.Menu
	build = func(items []Item) *menu.Menu {
		result := menu.NewMenu()
		for _, def := range items {
			if def.Label == "Window" && def.ID == "" && len(def.Children) == 0 {
				result.Append(menu.WindowMenu())
				continue
			}
			if def.Label == "-" {
				result.Append(menu.Separator())
				continue
			}
			if len(def.Children) > 0 {
				result.Append(menu.SubMenu(def.Label, build(def.Children)))
				continue
			}
			id := def.ID
			label := def.Label
			// Display existing shortcuts without registering a second global key
			// handler. Native accelerators otherwise steal editor/extension chords.
			if def.Key != "" {
				label += "\t" + def.Key
			}
			var accelerator *keys.Accelerator
			switch id {
			case "edit.cut":
				accelerator = keys.CmdOrCtrl("x")
			case "edit.copy":
				accelerator = keys.CmdOrCtrl("c")
			case "edit.paste":
				accelerator = keys.CmdOrCtrl("v")
			case "edit.selectAll":
				accelerator = keys.CmdOrCtrl("a")
			}
			if accelerator != nil {
				label = def.Label
			}
			item := menu.Text(label, accelerator, func(*menu.CallbackData) {
				c.mu.Lock()
				ctx, epoch, enabled := c.ctx, c.epoch, !c.items[id][0].Disabled
				c.mu.Unlock()
				if ctx != nil && enabled {
					if nativeEdit && nativeClipboard(id) {
						return
					}
					wr.EventsEmit(ctx, "native-menu:invoke", map[string]interface{}{"id": id, "epoch": epoch})
				}
			})
			item.Disabled = true
			c.items[id] = append(c.items[id], item)
			result.Append(item)
		}
		return result
	}
	definitions := Catalog()
	c.menu = build(definitions)
	c.taskViews = map[string]*menu.Menu{}
	for _, top := range c.menu.Items {
		if top.Label != "Message" {
			continue
		}
		c.taskMenu = top
		c.taskViews["mail"] = top.SubMenu
		var mailItems []*menu.MenuItem
		for _, item := range top.SubMenu.Items {
			switch item.Label {
			case "Event":
				c.taskViews["calendar"] = item.SubMenu
			case "Contact":
				c.taskViews["contacts"] = item.SubMenu
			default:
				mailItems = append(mailItems, item)
			}
		}
		for len(mailItems) > 0 && mailItems[len(mailItems)-1].Type == menu.SeparatorType {
			mailItems = mailItems[:len(mailItems)-1]
		}
		top.SubMenu.Items = mailItems
	}
	c.menu.Prepend(menu.AppMenu())
	return c
}

// Menu returns the Wails application menu owned by the controller.
func (c *Controller) Menu() *menu.Menu { return c.menu }

// Start connects menu state updates to the current frontend context.
func (c *Controller) Start(ctx context.Context) {
	c.mu.Lock()
	c.ctx = ctx
	c.mu.Unlock()
	wr.EventsOn(ctx, "native-menu:state", func(args ...interface{}) {
		if len(args) != 1 {
			return
		}
		raw, err := json.Marshal(args[0])
		if err != nil {
			return
		}
		var state State
		if json.Unmarshal(raw, &state) != nil {
			return
		}
		changed := c.applyState(state)
		if changed {
			wr.MenuUpdateApplicationMenu(ctx)
		}
	})
}

// applyState rejects old page states and updates all copies of an action.
func (c *Controller) applyState(state State) bool {
	enabled := map[string]bool{}
	for _, id := range state.Enabled {
		enabled[id] = true
	}
	c.mu.Lock()
	if state.Epoch < c.epoch {
		c.mu.Unlock()
		return false
	}
	c.epoch = state.Epoch
	changed := false
	view := state.View
	submenu, ok := c.taskViews[view]
	if !ok {
		view = "mail"
		submenu = c.taskViews[view]
	}
	label := map[string]string{"mail": "Message", "calendar": "Event", "contacts": "Contact"}[view]
	if c.taskMenu != nil && (c.taskMenu.Label != label || c.taskMenu.SubMenu != submenu) {
		c.taskMenu.Label = label
		c.taskMenu.SubMenu = submenu
		changed = true
	}
	for id, items := range c.items {
		disabled := !enabled[id]
		for _, item := range items {
			if item.Disabled != disabled {
				item.Disabled = disabled
				changed = true
			}
		}
	}
	c.mu.Unlock()
	return changed
}
