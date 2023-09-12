package sql

import "fmt"

func VacuumDB(db Executor) error {
	if _, err := db.Exec("vacuum", nil, nil); err != nil {
		return fmt.Errorf("vacuum %w", err)
	}
	return nil
}
