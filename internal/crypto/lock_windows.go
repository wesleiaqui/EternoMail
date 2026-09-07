package crypto

import (
 "os"
 "golang.org/x/sys/windows"
)

func lockKeyFile(f *os.File) error {
 return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{})
}
