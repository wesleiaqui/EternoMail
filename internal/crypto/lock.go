package crypto

import (
	"fmt"
	"os"
	"path/filepath"
)

// LockCredentialMigration serializes credential startup across processes.
// Closing the handle releases the OS lock even after an interrupted process.
// The lock file must never be removed: waiters may already hold its inode.
func LockCredentialMigration(dataDir string) (*os.File, error) {
	return acquireKeyLock(dataDir, ".credentials.lock")
}

func acquireKeyLock(dataDir, name string) (*os.File, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, name)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("key lock is not a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	// Check the opened inode against the directory entry before locking. This
	// rejects a symlink or replacement introduced between Lstat and OpenFile.
	info, err := os.Lstat(path)
	if err != nil {
		f.Close()
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, opened) {
		f.Close()
		return nil, fmt.Errorf("key lock changed while opening")
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if err := lockKeyFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
