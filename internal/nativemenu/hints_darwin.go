//go:build darwin && cgo

package nativemenu

/*
#cgo CFLAGS: -fblocks
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void postilaatikkoDecorateMenu(const char *json);
*/
import "C"
import (
	"encoding/json"
	"unsafe"
)

func (c *Controller) refreshNativeHints() {
	c.mu.Lock()
	data, err := json.Marshal(c.hintTree(c.menu.Items))
	c.mu.Unlock()
	if err != nil {
		return
	}
	value := C.CString(string(data))
	defer C.free(unsafe.Pointer(value))
	C.postilaatikkoDecorateMenu(value)
}

func menuLabel(label, _ string, _ bool) string { return label }
