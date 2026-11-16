//go:build darwin && cgo

package nativemenu

/*
#cgo CFLAGS: -fblocks
#cgo LDFLAGS: -framework Cocoa
void postilaatikkoClipboard(int command);
*/
import "C"

// nativeClipboard keeps AppKit's original image and rich-text clipboard types.
// Only clipboard operations go through the responder chain. Undo/Redo remain
// catalog actions owned by the composer's complete draft-history controller.
func nativeClipboard(id string) bool {
	commands := map[string]int{"edit.cut": 1, "edit.copy": 2, "edit.paste": 3, "edit.selectAll": 4}
	command, ok := commands[id]
	if !ok {
		return false
	}
	C.postilaatikkoClipboard(C.int(command))
	return true
}
