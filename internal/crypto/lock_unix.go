//go:build !windows

package crypto

import (
 "os"
 "golang.org/x/sys/unix"
)

func lockKeyFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX) }
