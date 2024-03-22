package node

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/natefinch/atomic"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	legacyKeyFileName       = "key.bin"
	keyDir                  = "identities"
	supervisedIDKeyFileName = "local.key"
)

// TestIdentity loads a pre-configured identity for testing purposes.
func (app *App) TestIdentity() ([]*signing.EdSigner, error) {
	if len(app.Config.TestConfig.SmesherKey) == 0 {
		return nil, nil
	}

	app.log.With().Error("!!!TESTING!!! using pre-configured smesher key")
	dst, err := hex.DecodeString(app.Config.TestConfig.SmesherKey)
	if err != nil {
		return nil, fmt.Errorf("decoding private key: %w", err)
	}
	signer, err := signing.NewEdSigner(
		signing.WithPrivateKey(dst),
		signing.WithPrefix(app.Config.Genesis.GenesisID().Bytes()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to construct test identity: %w", err)
	}

	app.log.With().Info("Loaded testing identity", signer.PublicKey())
	return []*signing.EdSigner{signer}, nil
}

// MigrateExistingIdentity migrates the legacy identity file to the new location.
//
// The legacy identity file is expected to be located at `app.Config.SMESHING.Opts.DataDir/key.bin`.
//
// TODO(mafa): this can be removed in a future version when the legacy identity file is no longer expected to exist.
func (app *App) MigrateExistingIdentity() error {
	oldKey := filepath.Join(app.Config.SMESHING.Opts.DataDir, legacyKeyFileName)
	app.log.Info("Looking for legacy identity file at `%v`", oldKey)

	src, err := os.Open(oldKey)
	switch {
	case errors.Is(err, os.ErrNotExist):
		app.log.Info("Legacy identity file not found.")
		return nil
	case err != nil:
		return fmt.Errorf("failed to open legacy identity file: %w", err)
	}
	defer src.Close()

	newKey := filepath.Join(app.Config.DataDir(), keyDir, supervisedIDKeyFileName)
	if err := os.MkdirAll(filepath.Dir(newKey), 0o700); err != nil {
		return fmt.Errorf("failed to create directory for identity file: %w", err)
	}

	_, err = os.Stat(newKey)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// no new key, migrate old to new
	case err == nil:
		// both exist, error
		return fmt.Errorf("%w: both %s and %s exist", fs.ErrExist, newKey, oldKey)
	case err != nil:
		return fmt.Errorf("stat %s: %w", newKey, err)
	}

	_, err = os.Stat(oldKey + ".bak")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// no backup, migrate old to new
	case err == nil:
		// backup already exists - something is wrong
		return fmt.Errorf("%w: backup %s already exists", fs.ErrExist, oldKey+".bak")
	case err != nil:
		return fmt.Errorf("stat %s: %w", oldKey+".bak", err)
	}

	dst, err := os.Create(newKey)
	if err != nil {
		return fmt.Errorf("failed to create new identity file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy identity file: %w", err)
	}

	if err := src.Close(); err != nil {
		return fmt.Errorf("failed to close legacy identity file: %w", err)
	}

	if err := atomic.ReplaceFile(oldKey, oldKey+".bak"); err != nil {
		return fmt.Errorf("failed to rename legacy identity file: %w", err)
	}

	app.log.Info("Migrated legacy identity file to `%v`", newKey)
	return nil
}

// NewIdentity creates a new identity, saves it to `keyDir/supervisedIDKeyFileName` in the config directory and
// initializes app.signers with that identity.
func (app *App) NewIdentity() error {
	dir := filepath.Join(app.Config.DataDir(), keyDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create directory for identity file: %w", err)
	}

	keyFile := filepath.Join(dir, supervisedIDKeyFileName)
	signer, err := signing.NewEdSigner(
		signing.WithPrefix(app.Config.Genesis.GenesisID().Bytes()),
		signing.ToFile(keyFile),
	)
	if err != nil {
		return fmt.Errorf("failed to create identity: %w", err)
	}

	app.log.With().Info("Created new identity",
		log.String("filename", supervisedIDKeyFileName),
		signer.PublicKey(),
	)
	app.signers = []*signing.EdSigner{signer}
	return nil
}

// LoadIdentities loads all existing identities from the config directory.
func (app *App) LoadIdentities() error {
	signers := make([]*signing.EdSigner, 0)

	dir := filepath.Join(app.Config.DataDir(), keyDir)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk directory at %s: %w", path, err)
		}

		// skip subdirectories and files in them
		if d.IsDir() && path != dir {
			return fs.SkipDir
		}

		// skip files that are not identity files
		if filepath.Ext(path) != ".key" {
			return nil
		}

		signer, err := signing.NewEdSigner(
			signing.FromFile(path),
			signing.WithPrefix(app.Config.Genesis.GenesisID().Bytes()),
		)
		if err != nil {
			return fmt.Errorf("failed to construct identity %s: %w", d.Name(), err)
		}

		app.log.With().Info("Loaded existing identity",
			log.String("filename", d.Name()),
			signer.PublicKey(),
		)
		signers = append(signers, signer)
		return nil
	})
	if err != nil {
		return err
	}
	if len(signers) == 0 {
		return fmt.Errorf("no identity files found: %w", fs.ErrNotExist)
	}

	// make sure all keys are unique
	seen := make(map[string]string)
	collision := false
	for _, sig := range signers {
		if file, ok := seen[sig.PublicKey().String()]; ok {
			app.log.With().Error("duplicate key",
				log.String("filename1", sig.Name()),
				log.String("filename2", file),
				sig.PublicKey(),
			)
			collision = true
			continue
		}
		seen[sig.PublicKey().String()] = sig.Name()
	}
	if collision {
		return errors.New("duplicate key found in identity files")
	}

	if len(signers) > 1 {
		app.log.Info("Loaded %d identities from disk", len(signers))
		for _, sig := range signers {
			if sig.Name() == supervisedIDKeyFileName {
				app.log.Error(
					"Identities contain key for supervised smeshing (%s). This is not supported in remote smeshing.",
					supervisedIDKeyFileName,
				)
				app.log.Error(
					"Ensure you do not have a file named %s in your identities directory when using remote smeshing.",
					supervisedIDKeyFileName,
				)
				return errors.New("supervised key found in remote smeshing mode")
			}
		}
	}

	app.signers = signers
	return nil
}
