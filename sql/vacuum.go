package sql

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/log"
)

func Vacuum(db Executor) error {
	log.Info("vacuuming db...")
	if _, err := db.Exec("vacuum", nil, nil); err != nil {
		return fmt.Errorf("vacuum %w", err)
	}
	log.Info("checkpointing db...")
	if _, err := db.Exec("pragma wal_checkpoint(TRUNCATE)", nil, nil); err != nil {
		return fmt.Errorf("wal checkpoint %w", err)
	}
	log.Info("db vacuum completed")
	return nil
}

func Analyze(db Executor) error {
	log.Info("analyzing db...")
	if _, err := db.Exec("analyze", nil, nil); err != nil {
		return fmt.Errorf("analyze %w", err)
	}
	log.Info("db analyze completed")
	return nil
}
