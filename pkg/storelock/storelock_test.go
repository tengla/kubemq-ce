package storelock

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAcquire_RefusesWhileHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "dir", "host.lock")
	first, err := Acquire(path)
	require.NoError(t, err)

	_, err = Acquire(path)
	require.ErrorIs(t, err, ErrHeld)

	first.Release()
	second, err := Acquire(path)
	require.NoError(t, err)
	second.Release()
}

func TestAcquire_LocksPerPath(t *testing.T) {
	dir := t.TempDir()
	first, err := Acquire(filepath.Join(dir, "a.lock"))
	require.NoError(t, err)
	defer first.Release()

	second, err := Acquire(filepath.Join(dir, "b.lock"))
	require.NoError(t, err)
	second.Release()
}
