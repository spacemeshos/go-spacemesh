package node

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	legacyKeyFileName       = "key.bin"
	keyDir                  = "identities"
	supervisedIDKeyFileName = "identity.key"
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
	if len(dst) != signing.PrivateKeySize {
		return nil, fmt.Errorf("invalid key size %d/%d", dst, signing.PrivateKeySize)
	}
	signer, err := signing.NewEdSigner(
		signing.WithPrivateKey(dst),
		signing.WithPrefix(app.Config.Genesis.GenesisID().Bytes()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to construct identity from data file: %w", err)
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

	if err := os.Rename(oldKey, oldKey+".bak"); err != nil {
		return fmt.Errorf("failed to rename legacy identity file: %w", err)
	}

	app.log.Info("Migrated legacy identity file to `%v`", newKey)
	return nil
}

// LoadIdentities loads all existing identities from the config directory.
func (app *App) LoadIdentities() error {
	signers := make([]*signing.EdSigner, 0)
	pubKeys := make(map[string]*signing.PublicKey)

	dir := filepath.Join(app.Config.DataDir(), keyDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create directory for identity file: %w", err)
	}
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

		// read hex data from file
		dst := make([]byte, signing.PrivateKeySize)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to open identity file at %s: %w", path, err)
		}

		n, err := hex.Decode(dst, data)
		if err != nil {
			return fmt.Errorf("decoding private key: %w", err)
		}
		if n != signing.PrivateKeySize {
			return fmt.Errorf("invalid key size %d/%d", n, signing.PrivateKeySize)
		}

		signer, err := signing.NewEdSigner(
			signing.WithPrivateKey(dst),
			signing.WithPrefix(app.Config.Genesis.GenesisID().Bytes()),
		)
		if err != nil {
			return fmt.Errorf("failed to construct identity from data file: %w", err)
		}

		app.log.With().Info("Loaded existing identity",
			log.String("filename", d.Name()),
			signer.PublicKey(),
		)
		signers = append(signers, signer)
		pubKeys[d.Name()] = signer.PublicKey()
		return nil
	})
	if err != nil {
		return err
	}

	// make sure all public keys are unique
	seen := make(map[string]string)
	collision := false
	for f1, pk := range pubKeys {
		if f2, ok := seen[pk.String()]; ok {
			app.log.With().Error("duplicate key",
				log.String("filename1", f1),
				log.String("filename2", f2),
				pk,
			)
			collision = true
			continue
		}
		seen[pk.String()] = f1
	}
	if collision {
		return fmt.Errorf("duplicate key found in identity files")
	}

	if len(signers) > 0 {
		app.signers = signers
		return nil
	}

	app.log.Info("Identity file not found. Creating new identity...")
	signer, err := signing.NewEdSigner(
		signing.WithPrefix(app.Config.Genesis.GenesisID().Bytes()),
	)
	if err != nil {
		return fmt.Errorf("failed to create identity: %w", err)
	}

	keyFile := filepath.Join(dir, supervisedIDKeyFileName)
	dst := make([]byte, hex.EncodedLen(len(signer.PrivateKey())))
	hex.Encode(dst, signer.PrivateKey())
	err = os.WriteFile(keyFile, dst, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write identity file: %w", err)
	}

	app.log.With().Info("Created new identity",
		log.String("filename", supervisedIDKeyFileName),
		signer.PublicKey(),
	)
	app.signers = []*signing.EdSigner{signer}
	return nil
}
