package node

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	legacyKeyFileName       = "key.bin"
	keyDir                  = "identities"
	supervisedIDKeyFileName = "local.key"
)

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
		log.ShortStringer("public_key", signer.PublicKey()),
	)
	app.signers = []*signing.EdSigner{signer}
	return nil
}

// LoadIdentities loads all existing identities from the config directory.
func (app *App) LoadIdentities() (err error) {
	app.signers, err = loadIdentities(app.Config.DataDir(), app.Config.Genesis.GenesisID().Bytes(), app.log)
	return err
}

func loadIdentities(path string, genesisId []byte, logger log.Log) ([]*signing.EdSigner, error) {
	signers := make([]*signing.EdSigner, 0)

	dir := filepath.Join(path, keyDir)
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
			signing.WithPrefix(genesisId),
		)
		if err != nil {
			return fmt.Errorf("failed to construct identity %s: %w", d.Name(), err)
		}

		logger.With().Info("Loaded existing identity",
			log.String("filename", d.Name()),
			log.ShortStringer("public_key", signer.PublicKey()),
		)
		signers = append(signers, signer)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(signers) == 0 {
		return nil, fmt.Errorf("no identity files found: %w", fs.ErrNotExist)
	}

	// make sure all keys are unique
	seen := make(map[string]string)
	collision := false
	for _, sig := range signers {
		if file, ok := seen[sig.PublicKey().String()]; ok {
			logger.With().Error("duplicate key",
				log.String("filename1", sig.Name()),
				log.String("filename2", file),
				log.String("public_key", sig.PublicKey().ShortString()),
			)
			collision = true
			continue
		}
		seen[sig.PublicKey().String()] = sig.Name()
	}
	if collision {
		return nil, errors.New("duplicate key found in identity files")
	}

	if len(signers) > 1 {
		logger.Info("Loaded %d identities from disk", len(signers))
		for _, sig := range signers {
			if sig.Name() == supervisedIDKeyFileName {
				logger.Error(
					"Identities contain key for supervised smeshing (%s). This is not supported in remote smeshing.",
					supervisedIDKeyFileName,
				)
				logger.Error(
					"Ensure you do not have a file named %s in your identities directory when using remote smeshing.",
					supervisedIDKeyFileName,
				)
				return nil, errors.New("supervised key found in remote smeshing mode")
			}
		}
	}
	return signers, nil
}
