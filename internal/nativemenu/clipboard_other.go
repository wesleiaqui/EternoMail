//go:build !darwin || !cgo

package nativemenu

// Other platforms keep the existing catalog-based edit bridge.
func nativeClipboard(string) bool { return false }
