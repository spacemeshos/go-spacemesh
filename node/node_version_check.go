package node

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func (app *App) verifyVersionUpgrades() error {
	return verifyLocalDbMigrations(app.Config)
}

// v1.5 requires going through v1.4 first as it removed in-code migrations 1 - 3.
func verifyLocalDbMigrations(cfg *config.Config) error {
	dbPath := filepath.Join(cfg.DataDir(), localDbFile)
	// if local DB doesnt exist, it's a fresh db and doesn't require in-code migrations
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil
	}
	version, err := sql.Version(dbPath)
	if err != nil {
		return fmt.Errorf("failed to get db version: %w", err)
	}
	if version == 0 || version >= 3 {
		return nil
	}
	return errors.New("to use node version 1.5 or newer, please upgrade to v1.4 first")
}
