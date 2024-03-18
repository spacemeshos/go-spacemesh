package node

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func setupAppWithKeys(tb testing.TB, data ...[]byte) (*App, *observer.ObservedLogs) {
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zap.New(observer)
	app := New(WithLog(log.NewFromLog(logger)))
	app.Config.DataDirParent = tb.TempDir()
	if len(data) == 0 {
		return app, observedLogs
	}

	require.NoError(tb, os.MkdirAll(filepath.Join(app.Config.DataDirParent, keyDir), 0o700))
	if len(data) == 1 {
		key := data[0]
		keyFile := filepath.Join(app.Config.DataDirParent, keyDir, supervisedIDKeyFileName)
		require.NoError(tb, os.WriteFile(keyFile, key, 0o600))
		return app, observedLogs
	}

	for i, key := range data {
		keyFile := filepath.Join(app.Config.DataDirParent, keyDir, fmt.Sprintf("identity_%d.key", i))
		require.NoError(tb, os.WriteFile(keyFile, key, 0o600))
	}
	return app, observedLogs
}

func TestSpacemeshApp_NewIdentity(t *testing.T) {
	t.Run("no key", func(t *testing.T) {
		app := New(WithLog(logtest.New(t)))
		app.Config.DataDirParent = t.TempDir()
		err := app.NewIdentity()
		require.NoError(t, err)
		require.Len(t, app.signers, 1)
		require.FileExists(t, filepath.Join(app.Config.DataDirParent, keyDir, supervisedIDKeyFileName))
	})

	t.Run("no key but existing directory", func(t *testing.T) {
		app := New(WithLog(logtest.New(t)))
		app.Config.DataDirParent = t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(app.Config.DataDirParent, keyDir), 0o700))
		err := app.NewIdentity()
		require.NoError(t, err)
		require.Len(t, app.signers, 1)
		require.FileExists(t, filepath.Join(app.Config.DataDirParent, keyDir, supervisedIDKeyFileName))
	})

	t.Run("existing key is not overwritten", func(t *testing.T) {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		before := signer.PublicKey()

		app, _ := setupAppWithKeys(t, []byte(hex.EncodeToString(signer.PrivateKey())))
		err = app.NewIdentity()
		require.ErrorContains(t, err, fmt.Sprintf("identity file %s already exists", supervisedIDKeyFileName))
		require.ErrorIs(t, err, fs.ErrExist)
		require.Empty(t, app.signers)

		err = app.LoadIdentities()
		require.NoError(t, err)
		require.NotEmpty(t, app.signers)
		after := app.signers[0].PublicKey()
		require.Equal(t, before, after)
	})

	t.Run("non default key is preserved on disk", func(t *testing.T) {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		existingKey := signer.PublicKey()

		app, _ := setupAppWithKeys(t, []byte(hex.EncodeToString(signer.PrivateKey())))
		err = os.Rename(
			filepath.Join(app.Config.DataDirParent, keyDir, supervisedIDKeyFileName),
			filepath.Join(app.Config.DataDirParent, keyDir, "do_not_delete.key"),
		)
		require.NoError(t, err)

		err = app.NewIdentity()
		require.NoError(t, err)
		require.Len(t, app.signers, 1)
		newKey := app.signers[0].PublicKey()
		require.NotEqual(t, existingKey, newKey) // new key was created and loaded
	})
}

func TestSpacemeshApp_LoadIdentities(t *testing.T) {
	t.Run("no key", func(t *testing.T) {
		app := New(WithLog(logtest.New(t)))
		app.Config.DataDirParent = t.TempDir()
		err := app.LoadIdentities()
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.Empty(t, app.signers)
	})

	t.Run("no key but existing directory", func(t *testing.T) {
		app := New(WithLog(logtest.New(t)))
		app.Config.DataDirParent = t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(app.Config.DataDirParent, keyDir), 0o700))
		err := app.LoadIdentities()
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.Empty(t, app.signers)
	})

	t.Run("good key", func(t *testing.T) {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)

		app, _ := setupAppWithKeys(t, []byte(hex.EncodeToString(signer.PrivateKey())))
		err = app.LoadIdentities()
		require.NoError(t, err)
		require.NotEmpty(t, app.signers)
		before := app.signers[0].PublicKey()

		err = app.LoadIdentities()
		require.NoError(t, err)
		require.NotEmpty(t, app.signers)
		after := app.signers[0].PublicKey()
		require.Equal(t, before, after)
	})

	t.Run("bad length", func(t *testing.T) {
		app, _ := setupAppWithKeys(t, bytes.Repeat([]byte("ab"), signing.PrivateKeySize-1))
		err := app.LoadIdentities()
		require.ErrorContains(t, err, fmt.Sprintf("invalid key size 63/64 for %s", supervisedIDKeyFileName))
		require.Nil(t, app.signers)
	})

	t.Run("bad hex", func(t *testing.T) {
		app, _ := setupAppWithKeys(t, bytes.Repeat([]byte("CV"), signing.PrivateKeySize))
		err := app.LoadIdentities()
		require.ErrorContains(t, err, fmt.Sprintf("decoding private key in %s:", supervisedIDKeyFileName))
		require.ErrorIs(t, err, hex.InvalidByteError(byte('V')))
		require.Nil(t, app.signers)
	})

	t.Run("duplicate keys", func(t *testing.T) {
		key1, err := signing.NewEdSigner()
		require.NoError(t, err)
		key2, err := signing.NewEdSigner()
		require.NoError(t, err)

		app, observedLogs := setupAppWithKeys(t,
			[]byte(hex.EncodeToString(key1.PrivateKey())),
			[]byte(hex.EncodeToString(key2.PrivateKey())),
			[]byte(hex.EncodeToString(key1.PrivateKey())),
			[]byte(hex.EncodeToString(key2.PrivateKey())),
		)
		err = app.LoadIdentities()
		require.ErrorContains(t, err, "duplicate key")

		require.Len(t, observedLogs.All(), 2)
		log1 := observedLogs.FilterField(zap.String("public_key", key1.PublicKey().ShortString()))
		require.Len(t, log1.All(), 1)
		require.Contains(t, log1.All()[0].Message, "duplicate key")
		log2 := observedLogs.FilterField(zap.String("public_key", key2.PublicKey().ShortString()))
		require.Len(t, log2.All(), 1)
		require.Contains(t, log2.All()[0].Message, "duplicate key")
	})

	t.Run("supervised key alongside others", func(t *testing.T) {
		key, err := signing.NewEdSigner()
		require.NoError(t, err)
		key1, err := signing.NewEdSigner()
		require.NoError(t, err)
		key2, err := signing.NewEdSigner()
		require.NoError(t, err)

		app, _ := setupAppWithKeys(t,
			[]byte(hex.EncodeToString(key1.PrivateKey())),
			[]byte(hex.EncodeToString(key2.PrivateKey())),
		)

		dst := make([]byte, hex.EncodedLen(len(key.PrivateKey())))
		hex.Encode(dst, key.PrivateKey())
		err = os.WriteFile(filepath.Join(app.Config.DataDirParent, keyDir, supervisedIDKeyFileName), dst, 0o600)
		require.NoError(t, err)
		err = app.LoadIdentities()
		require.ErrorContains(t, err, "supervised key found in remote smeshing mode")
	})

	t.Run("multiple good keys", func(t *testing.T) {
		key1, err := signing.NewEdSigner()
		require.NoError(t, err)
		key2, err := signing.NewEdSigner()
		require.NoError(t, err)
		key3, err := signing.NewEdSigner()
		require.NoError(t, err)

		app, _ := setupAppWithKeys(t,
			[]byte(hex.EncodeToString(key1.PrivateKey())),
			[]byte(hex.EncodeToString(key2.PrivateKey())),
			[]byte(hex.EncodeToString(key3.PrivateKey())),
		)

		err = app.LoadIdentities()
		require.NoError(t, err)
		require.Len(t, app.signers, 3)
	})
}

func Test_MigrateExistingIdentity(t *testing.T) {
	t.Run("no key - no migration", func(t *testing.T) {
		app := New(WithLog(logtest.New(t)))
		app.Config.DataDirParent = t.TempDir()
		app.Config.SMESHING.Opts.DataDir = t.TempDir()
		err := app.MigrateExistingIdentity()
		require.NoError(t, err)
	})

	t.Run("existing key is migrated", func(t *testing.T) {
		app := New(WithLog(logtest.New(t)))
		app.Config.DataDirParent = t.TempDir()
		app.Config.SMESHING.Opts.DataDir = t.TempDir()

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		err = os.WriteFile(filepath.Join(app.Config.SMESHING.Opts.DataDir, legacyKeyFileName), sig.PrivateKey(), 0o600)
		require.NoError(t, err)

		err = app.MigrateExistingIdentity()
		require.NoError(t, err)

		require.FileExists(t, filepath.Join(app.Config.DataDirParent, keyDir, supervisedIDKeyFileName))
		require.NoFileExists(t, filepath.Join(app.Config.SMESHING.Opts.DataDir, legacyKeyFileName))
		require.FileExists(t, filepath.Join(app.Config.SMESHING.Opts.DataDir, legacyKeyFileName+".bak"))
	})

	t.Run("migration does not overwrite existing key", func(t *testing.T) {
		app := New(WithLog(logtest.New(t)))
		app.Config.DataDirParent = t.TempDir()
		app.Config.SMESHING.Opts.DataDir = t.TempDir()

		sigOld, err := signing.NewEdSigner()
		require.NoError(t, err)

		oldKey := filepath.Join(app.Config.SMESHING.Opts.DataDir, legacyKeyFileName)
		err = os.WriteFile(oldKey, sigOld.PrivateKey(), 0o600)
		require.NoError(t, err)

		sigNew, err := signing.NewEdSigner()
		require.NoError(t, err)

		newKey := filepath.Join(app.Config.DataDirParent, keyDir, supervisedIDKeyFileName)
		err = os.MkdirAll(filepath.Dir(newKey), 0o700)
		require.NoError(t, err)
		err = os.WriteFile(newKey, sigNew.PrivateKey(), 0o600)
		require.NoError(t, err)

		err = app.MigrateExistingIdentity()
		require.ErrorIs(t, err, fs.ErrExist)
		require.ErrorContains(t, err, fmt.Sprintf("both %s and %s exist", newKey, oldKey))
		require.FileExists(t, newKey)
		require.FileExists(t, oldKey)

		newKeyBin, err := os.ReadFile(newKey)
		require.NoError(t, err)
		require.Equal(t, []byte(sigNew.PrivateKey()), newKeyBin)

		oldKeyBin, err := os.ReadFile(oldKey)
		require.NoError(t, err)
		require.Equal(t, []byte(sigOld.PrivateKey()), oldKeyBin)
	})

	t.Run("migrate with an already existing backup", func(t *testing.T) {
		app := New(WithLog(logtest.New(t)))
		app.Config.DataDirParent = t.TempDir()
		app.Config.SMESHING.Opts.DataDir = t.TempDir()

		sigOld, err := signing.NewEdSigner()
		require.NoError(t, err)

		oldKey := filepath.Join(app.Config.SMESHING.Opts.DataDir, legacyKeyFileName)
		err = os.WriteFile(oldKey, sigOld.PrivateKey(), 0o600)
		require.NoError(t, err)

		sigBackup, err := signing.NewEdSigner()
		require.NoError(t, err)

		backupKey := filepath.Join(app.Config.SMESHING.Opts.DataDir, legacyKeyFileName+".bak")
		err = os.WriteFile(backupKey, sigBackup.PrivateKey(), 0o600)
		require.NoError(t, err)

		err = app.MigrateExistingIdentity()
		require.ErrorIs(t, err, fs.ErrExist)
		require.ErrorContains(t, err, fmt.Sprintf("backup %s already exists", backupKey))
		require.FileExists(t, filepath.Join(app.Config.SMESHING.Opts.DataDir, legacyKeyFileName))
		require.FileExists(t, filepath.Join(app.Config.SMESHING.Opts.DataDir, legacyKeyFileName+".bak"))

		oldKeyBin, err := os.ReadFile(oldKey)
		require.NoError(t, err)
		require.Equal(t, []byte(sigOld.PrivateKey()), oldKeyBin)

		backupKeyBin, err := os.ReadFile(backupKey)
		require.NoError(t, err)
		require.Equal(t, []byte(sigBackup.PrivateKey()), backupKeyBin)
	})
}
