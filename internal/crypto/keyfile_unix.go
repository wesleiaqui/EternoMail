//go:build !windows

package crypto

import "os"

func renameKeyFile(from, to string) error { return os.Rename(from, to) }
