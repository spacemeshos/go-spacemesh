package vacuum

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/sql"
)

func VacuumDB(db sql.Executor) error {
	if _, err := db.Exec("vacuum", nil, nil); err != nil {
		return fmt.Errorf("vacuum %w", err)
	}
	if _, err := db.Exec("pragma wal_checkpoint(TRUNCATE)", nil, nil); err != nil {
		return fmt.Errorf("wal checkpoint %w", err)
	}
	return nil
}

func FreePct(db sql.Executor) (int, error) {
	var pct int
	if _, err := db.Exec(`SELECT freelist_count, page_count FROM pragma_freelist_count(), pragma_page_count();`,
		nil, func(stmt *sql.Statement) bool {
			pct = 100 * stmt.ColumnInt(0) / stmt.ColumnInt(1)
			return false
		}); err != nil {
		return 0, fmt.Errorf("db page size %w", err)
	}
	return pct, nil
}
