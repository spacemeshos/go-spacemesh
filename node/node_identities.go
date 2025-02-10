package node

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
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
	signer, err := newIdentity(app.Config.DataDir(), app.Config.Genesis.GenesisID(), app.log.Zap())
	if err != nil {
		return err
	}
	app.signers = []*signing.EdSigner{signer}
	return nil
}

func newIdentity(datadir string, genesisID types.Hash20, logger *zap.Logger) (*signing.EdSigner, error) {
	dir := filepath.Join(datadir, keyDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create directory for identity file: %w", err)
	}

	keyFile := filepath.Join(dir, supervisedIDKeyFileName)
	signer, err := signing.NewEdSigner(
		signing.WithPrefix(genesisID.Bytes()),
		signing.ToFile(keyFile),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create identity: %w", err)
	}

	logger.Info("Created new identity",
		zap.String("filename", supervisedIDKeyFileName),
		log.ZShortStringer("public_key", signer.PublicKey()),
	)
	return signer, nil
}

// LoadIdentities loads all existing identities from the config directory.
func (app *App) LoadIdentities() error {
	signers, err := loadIdentities(app.Config.DataDir(), app.Config.Genesis.GenesisID(), app.log.Zap())
	if err != nil {
		return err
	}
	app.signers = signers
	return nil
}

func loadIdentities(datadir string, genesisID types.Hash20, logger *zap.Logger) ([]*signing.EdSigner, error) {
	signers := make([]*signing.EdSigner, 0)

	dir := filepath.Join(datadir, keyDir)
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
			signing.WithPrefix(genesisID.Bytes()),
		)
		if err != nil {
			return fmt.Errorf("failed to construct identity %s: %w", d.Name(), err)
		}

		logger.Info("Loaded existing identity",
			zap.String("filename", d.Name()),
			log.ZShortStringer("public_key", signer.PublicKey()),
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
			logger.Error("duplicate key",
				zap.String("filename1", sig.Name()),
				zap.String("filename2", file),
				zap.String("public_key", sig.PublicKey().ShortString()),
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
		logger.Sugar().Infof("Loaded %d identities from disk", len(signers))
		for _, sig := range signers {
			if sig.Name() == supervisedIDKeyFileName {
				logger.Sugar().Errorf(
					"Identities contain key for supervised smeshing (%s). This is not supported in remote smeshing.",
					supervisedIDKeyFileName,
				)
				logger.Sugar().Errorf(
					"Ensure you do not have a file named %s in your identities directory when using remote smeshing.",
					supervisedIDKeyFileName,
				)
				return nil, errors.New("supervised key found in remote smeshing mode")
			}
		}
	}

	return signers, nil
}
