package sql

import (
	"fmt"
)

func Vacuum(db Executor) error {
	if _, err := db.Exec("vacuum", nil, nil); err != nil {
		return fmt.Errorf("vacuum %w", err)
	}
	if _, err := db.Exec("pragma wal_checkpoint(TRUNCATE)", nil, nil); err != nil {
		return fmt.Errorf("wal checkpoint %w", err)
	}
	return nil
}
