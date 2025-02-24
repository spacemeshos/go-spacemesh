package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLock(t *testing.T) {
	t.Run("creates lock directory if not exists", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "subdir", "node.lock")

		unlock, err := lock(lockPath)
		require.NoError(t, err)
		defer unlock()

		// Verify directory was created
		_, err = os.Stat(filepath.Dir(lockPath))
		require.NoError(t, err)
	})
	t.Run("prevents multiple locks", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "node.lock")

		// First lock
		unlock1, err := lock(lockPath)
		require.NoError(t, err)
		defer unlock1()

		// Second lock attempt should fail
		unlock2, err := lock(lockPath)
		require.Error(t, err)
		require.Nil(t, unlock2)
		require.Contains(t, err.Error(), "only one spacemesh instance should be running")
	})
	t.Run("unlock releases the lock", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "node.lock")

		// First lock
		unlock1, err := lock(lockPath)
		require.NoError(t, err)

		// Release first lock
		require.NoError(t, unlock1())

		// Should be able to acquire second lock
		unlock2, err := lock(lockPath)
		require.NoError(t, err)
		defer unlock2()
	})
	t.Run("unlock is idempotent", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "node.lock")

		unlock, err := lock(lockPath)
		require.NoError(t, err)

		// First unlock
		require.NoError(t, unlock())

		// Second unlock should not error
		require.NoError(t, unlock())
	})
}
