package filewatch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func upper(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty")
	}
	return strings.ToUpper(string(data)), nil
}

func TestFile_Reload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	var reloadErr error
	f, err := New(path, time.Millisecond, upper, func(err error) { reloadErr = err })
	require.NoError(t, err)
	require.Equal(t, "ONE", f.Get())

	require.NoError(t, os.WriteFile(path, []byte("three"), 0o600))
	time.Sleep(5 * time.Millisecond)
	require.Equal(t, "THREE", f.Get())

	// A bad edit keeps the last good value and reports the error.
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	time.Sleep(5 * time.Millisecond)
	require.Equal(t, "THREE", f.Get())
	require.Error(t, reloadErr)
}

func TestFile_NoReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o600))
	f, err := New(path, 0, upper, nil)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("three"), 0o600))
	time.Sleep(5 * time.Millisecond)
	require.Equal(t, "ONE", f.Get())
}

func TestFile_Errors(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "missing"), 0, upper, nil)
	require.Error(t, err)
	path := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	_, err = New(path, 0, upper, nil)
	require.Error(t, err)
}
