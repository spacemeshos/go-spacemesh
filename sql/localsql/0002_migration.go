package localsql

import (
	"fmt"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/spacemeshos/post/initialization"

	"github.com/spacemeshos/go-spacemesh/sql"
)

func New0002Migration(dataDir string) *migration0002 {
	return &migration0002{dataDir: dataDir}
}

type migration0002 struct {
	dataDir string
}

func (migration0002) Name() string {
	return "extend initial post"
}

func (migration0002) Order() int {
	return 2
}

func (m migration0002) Apply(db sql.Executor) error {
	_, err := db.Exec("ALTER TABLE initial_post ADD COLUMN num_units UNSIGNED INT;", nil, nil)
	if err != nil {
		return fmt.Errorf("add column num_units to initial_post: %w", err)
	}
	_, err = db.Exec("ALTER TABLE initial_post ADD COLUMN vrf_nonce UNSIGNED LONG INT;", nil, nil)
	if err != nil {
		return fmt.Errorf("add column vrf_nonce to initial_post: %w", err)
	}

	// count count in initial_post
	var count int
	_, err = db.Exec("SELECT COUNT(*) FROM initial_post;", nil, func(stmt *sqlite.Stmt) bool {
		count = int(stmt.ColumnInt64(0))
		return true
	})

	if count > 0 {
		// update num_units and vrf_nonce with data from post metadata file
		meta, err := initialization.LoadMetadata(m.dataDir)
		if err != nil {
			return fmt.Errorf("load post metadata: %w", err)
		}

		rows, err := db.Exec(`
			UPDATE initial_post SET
				num_units = ?2, commit_atx = ?3, vrf_nonce = ?4
			where id = ?1 returning id;`,
			func(stmt *sqlite.Stmt) {
				stmt.BindBytes(1, meta.NodeId)
				stmt.BindInt64(2, int64(meta.NumUnits))
				stmt.BindBytes(3, meta.CommitmentAtxId)
				stmt.BindInt64(4, int64(*meta.Nonce))
			}, nil)
		if err != nil {
			return fmt.Errorf("update num_units and vrf_nonce in initial_post: %w", err)
		}
		if rows > 1 {
			return fmt.Errorf("expected to update 0 or 1 row in initial_post, updated %d rows", rows)
		}
	}

	// adding not null constraint to commit_atx, num_units and vrf_nonce columns is a multi-step process:
	// 1. create new table with same schema and new constraints
	if _, err := db.Exec(`
		CREATE TABLE initial_post_new
		(
			id            CHAR(32) PRIMARY KEY,
			post_nonce    UNSIGNED INT NOT NULL,
			post_indices  VARCHAR NOT NULL,
			post_pow      UNSIGNED LONG INT NOT NULL,

			num_units     UNSIGNED INT NOT NULL,
			commit_atx    CHAR(32) NOT NULL,
			vrf_nonce     UNSIGNED LONG INT NOT NULL
		);`, nil, nil); err != nil {
		return fmt.Errorf("create initial_post_new table: %w", err)
	}

	// 2. copy data from old table to new table
	if _, err := db.Exec(`
		INSERT INTO initial_post_new (
			id, post_nonce, post_indices, post_pow, commit_atx, num_units, vrf_nonce
		) SELECT
			id, post_nonce, post_indices, post_pow, commit_atx, num_units, vrf_nonce
		FROM initial_post;`, nil, nil); err != nil {
		return fmt.Errorf("copy data from initial_post to initial_post_new: %w", err)
	}

	// 3. drop old table
	if _, err := db.Exec("DROP TABLE initial_post;", nil, nil); err != nil {
		return fmt.Errorf("drop initial_post table: %w", err)
	}

	// 4. rename new table to old table
	if _, err := db.Exec("ALTER TABLE initial_post_new RENAME TO initial_post;", nil, nil); err != nil {
		return fmt.Errorf("rename initial_post_new to initial_post: %w", err)
	}

	return err
}
