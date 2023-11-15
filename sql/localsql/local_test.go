package localsql

import (
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql"
)

func fileHash(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := sha256.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return nil, err
	}

	return h.Sum(nil), nil
}

func TestDatabase_MigrateTwice_NoOp(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.db")
	db, err := Open("file:"+file,
		sql.WithMigration(New0002Migration(t.TempDir())),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	old, err := fileHash(file)
	require.NoError(t, err)

	db, err = Open("file:"+file,
		sql.WithMigration(New0002Migration(t.TempDir())),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	new, err := fileHash(file)
	require.NoError(t, err)

	require.Equal(t, old, new)
}
