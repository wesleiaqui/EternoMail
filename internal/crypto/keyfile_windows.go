package crypto

import "golang.org/x/sys/windows"

// Windows cannot fsync a directory with os.File. Request synchronous rename
// durability before replacing device.key or committing credentials in SQLite.
func renameKeyFile(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	destination, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, destination, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
