//go:build !darwin || !cgo

package nativemenu

// Other platforms retain their existing menu presentation and keyboard routing.
func (c *Controller) refreshNativeHints() {}

func menuLabel(label, shortcut string, accelerator bool) string {
	if shortcut != "" && !accelerator {
		return label + "\t" + shortcut
	}
	return label
}
