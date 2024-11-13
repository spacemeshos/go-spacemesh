package migrations

import (
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type migration0021 struct {
	batch int
}

var _ sql.Migration = &migration0021{}

func New0021Migration(batch int) *migration0021 {
	return &migration0021{
		batch: batch,
	}
}

func (*migration0021) Name() string {
	return "populate posts table with units for each ATX"
}

func (*migration0021) Order() int {
	return 21
}

func (*migration0021) Rollback() error {
	return nil
}

func (m *migration0021) Apply(db sql.Executor, logger *zap.Logger) error {
	if err := m.applySql(db); err != nil {
		return err
	}
	var total int
	_, err := db.Exec("SELECT count(*) FROM atx_blobs", nil, func(s *sql.Statement) bool {
		total = s.ColumnInt(0)
		return false
	})
	if err != nil {
		return fmt.Errorf("counting all ATXs %w", err)
	}
	logger.Info("applying migration 21", zap.Int("total", total))

	for offset := 0; ; offset += m.batch {
		n, err := m.processBatch(db, offset, m.batch)
		if err != nil {
			return err
		}

		processed := offset + n
		progress := float64(processed) * 100.0 / float64(total)
		logger.Info("processed ATXs", zap.Float64("progress [%]", progress))
		if processed >= total {
			break
		}
	}

	if err := m.applyIndices(db); err != nil {
		return err
	}

	return nil
}

const tableSQL = `CREATE TABLE posts (
    atxid CHAR(32) NOT NULL,
    pubkey CHAR(32) NOT NULL,
    prev_atxid CHAR(32),
    prev_atx_index INT,
    units INT NOT NULL
);`

func (m *migration0021) applySql(db sql.Executor) error {
	_, err := db.Exec(tableSQL, nil, nil)
	if err != nil {
		return fmt.Errorf("creating posts table: %w", err)
	}

	return nil
}

func (m *migration0021) applyIndices(db sql.Executor) error {
	query := "CREATE UNIQUE INDEX posts_by_atxid_by_pubkey ON posts (atxid, pubkey);"
	_, err := db.Exec(query, nil, nil)
	if err != nil {
		return fmt.Errorf("creating index `posts_by_atxid_by_pubkey`: %w", err)
	}

	query = "CREATE INDEX posts_by_atxid_by_pubkey_prev_atxid ON posts (atxid, pubkey, prev_atxid);"
	_, err = db.Exec(query, nil, nil)
	if err != nil {
		return fmt.Errorf("creating index `posts_by_atxid_by_pubkey`: %w", err)
	}

	query = "ALTER TABLE atxs DROP COLUMN prev_id;"
	_, err = db.Exec(query, nil, nil)
	if err != nil {
		return fmt.Errorf("dropping column `prev_id` from `atxs`: %w", err)
	}
	return nil
}

type update struct {
	id    types.NodeID
	prev  types.ATXID
	units uint32
}

func (m *migration0021) processBatch(db sql.Executor, offset, size int) (int, error) {
	var blob sql.Blob
	var id types.ATXID
	var procErr error
	updates := make(map[types.ATXID]*update)
	rows, err := db.Exec("SELECT id, atx, version FROM atx_blobs LIMIT ?1 OFFSET ?2",
		func(s *sql.Statement) {
			s.BindInt64(1, int64(size))
			s.BindInt64(2, int64(offset))
		},
		func(stmt *sql.Statement) bool {
			_, procErr = stmt.ColumnReader(0).Read(id[:])
			if procErr != nil {
				return false
			}

			blob.FromColumn(stmt, 1)
			version := types.AtxVersion(stmt.ColumnInt(2))

			upd, err := processATX(types.AtxBlob{Blob: blob.Bytes, Version: version})
			if err != nil {
				procErr = fmt.Errorf("processing ATX %s: %w", id, err)
				return false
			}
			updates[id] = upd
			return true
		},
	)

	if err := errors.Join(err, procErr); err != nil {
		return 0, fmt.Errorf("getting ATX blobs: %w", err)
	}
	if rows == 0 {
		return 0, nil
	}

	if err := m.applyPendingUpdates(db, updates); err != nil {
		return 0, fmt.Errorf("applying updates: %w", err)
	}
	return rows, nil
}

func (m *migration0021) applyPendingUpdates(db sql.Executor, updates map[types.ATXID]*update) error {
	for atxID, upd := range updates {
		if err := setPost(db, atxID, upd.prev, 0, upd.id, upd.units); err != nil {
			return err
		}
	}
	return nil
}

func processATX(blob types.AtxBlob) (*update, error) {
	// The migration adding the version column does not set it to 1 for existing ATXs.
	// Thus, both values 0 and 1 mean V1.
	switch blob.Version {
	case 0:
		fallthrough
	case types.AtxV1:
		var watx wire.ActivationTxV1
		if err := codec.Decode(blob.Blob, &watx); err != nil {
			return nil, fmt.Errorf("decoding ATX V1: %w", err)
		}
		return &update{watx.SmesherID, watx.PrevATXID, watx.NumUnits}, nil
	default:
		return nil, fmt.Errorf("unsupported ATX version: %d", blob.Version)
	}
}

func setPost(db sql.Executor, atxID, prev types.ATXID, prevIndex int, id types.NodeID, units uint32) error {
	_, err := db.Exec(
		`INSERT INTO posts (atxid, pubkey, prev_atxid, prev_atx_index, units) VALUES (?1, ?2, ?3, ?4, ?5);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, atxID.Bytes())
			stmt.BindBytes(2, id.Bytes())
			if prev != types.EmptyATXID {
				stmt.BindBytes(3, prev.Bytes())
			}
			stmt.BindInt64(4, int64(prevIndex))
			stmt.BindInt64(5, int64(units))
		},
		nil,
	)
	return err
}
