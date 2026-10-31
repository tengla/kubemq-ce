// Package storelock keeps two servers from writing to one store. The lock belongs
// to the open file, so the operating system releases it when the process dies,
// and a lock file left behind by a crash doesn't block anything.
package storelock

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrHeld means another open of the lock file holds the lock.
var ErrHeld = errors.New("store is in use")

type Lock struct {
	file *os.File
}

// Acquire takes the lock in path, creating the file and its directory if needed.
// It doesn't wait: if the lock is held, it returns ErrHeld.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &Lock{file: file}, nil
}

// Release gives up the lock. The file stays, since removing it could split the
// lock between a server holding the old file and one creating a new file.
func (l *Lock) Release() {
	_ = l.file.Close()
}
