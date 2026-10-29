// Package filewatch keeps the parsed contents of a file that operators may
// edit while the server runs, such as a token revocation list. Callers poll it
// from the request path; it re-reads the file at most once per interval and
// only when its size or modification time changed, so it needs no goroutine.
package filewatch

import (
	"fmt"
	"os"
	"sync"
	"time"
)

type File[T any] struct {
	path     string
	interval time.Duration
	parse    func([]byte) (T, error)
	onError  func(error)

	mu      sync.Mutex
	value   T
	checked time.Time
	modTime time.Time
	size    int64
}

// New reads and parses the file once, failing if either fails. Later reload
// errors go to onError, which may be nil, and keep the last good value. An
// interval of 0 or less never reloads.
func New[T any](path string, interval time.Duration, parse func([]byte) (T, error), onError func(error)) (*File[T], error) {
	f := &File[T]{path: path, interval: interval, parse: parse, onError: onError}
	if err := f.load(); err != nil {
		return nil, err
	}
	return f, nil
}

// Get returns the current value, reloading the file first if it is due.
func (f *File[T]) Get() T {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.interval > 0 && time.Since(f.checked) >= f.interval {
		if err := f.load(); err != nil && f.onError != nil {
			f.onError(err)
		}
	}
	return f.value
}

// load must be called with f.mu held, or before f is shared.
func (f *File[T]) load() error {
	f.checked = time.Now()
	info, err := os.Stat(f.path)
	if err != nil {
		return err
	}
	if info.Size() == f.size && info.ModTime().Equal(f.modTime) {
		return nil
	}
	data, err := os.ReadFile(f.path)
	if err != nil {
		return err
	}
	value, err := f.parse(data)
	if err != nil {
		return fmt.Errorf("%s: %w", f.path, err)
	}
	f.value, f.size, f.modTime = value, info.Size(), info.ModTime()
	return nil
}
