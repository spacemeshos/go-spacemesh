package identities

import (
	"context"
	"errors"
	"fmt"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	// Bloom filter size is < 234 KiB while below 100k identities.
	BloomFilterFalsePositiveRate = 0.0001
	BloomFilterMinSize           = 100000
	BloomFilterExtraCoef         = 1.2
)

// StartBloomFilter intializes and loads the bloom filter for malicious identities.
func StartBloomFilter(db sql.StateDatabase, logger *zap.Logger) *sql.DBBloomFilter {
	bf := sql.NewDBBloomFilter(
		logger, "malicious",
		`SELECT i1.pubkey FROM identities i1
 		LEFT JOIN identities i2 ON i1.marriage_atx = i2.marriage_atx
		WHERE i1.proof IS NOT NULL OR i2.proof IS NOT NULL`,
		"i1.pubkey",
		BloomFilterMinSize, BloomFilterExtraCoef, BloomFilterFalsePositiveRate)
	bf.Start(db)
	db.AddSet(bf)
	return bf
}

// SetMalicious records identity as malicious.
func SetMalicious(db sql.Executor, nodeID types.NodeID, proof []byte, received time.Time) error {
	_, err := db.Exec(`insert into identities (pubkey, proof, received)
	values (?1, ?2, ?3)
	ON CONFLICT(pubkey) DO UPDATE SET proof = excluded.proof
	WHERE proof IS NULL;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
			stmt.BindBytes(2, proof)
			stmt.BindInt64(3, received.UnixNano())
		}, nil,
	)
	if err != nil {
		return fmt.Errorf("set malicious %v: %w", nodeID, err)
	}
	ids, err := EquivocationSet(db, nodeID)
	if err != nil {
		return fmt.Errorf("get equivocation set for %v: %w", nodeID, err)
	}
	for _, id := range ids {
		sql.AddToSet(db, "malicious", id[:])
	}
	return nil
}

// IsMalicious returns true if identity is known to be malicious.
func IsMalicious(db sql.Executor, nodeID types.NodeID) (bool, error) {
	has, err := sql.Contains(db, "malicious", nodeID[:])
	if err == nil {
		return has, nil
	} else if !errors.Is(err, sql.ErrNoSet) {
		return false, fmt.Errorf("check if node %s is malicious: %w", nodeID, err)
	}

	rows, err := db.Exec(`
	SELECT 1 FROM identities
	WHERE (
		marriage_atx = (SELECT marriage_atx FROM identities WHERE pubkey = ?1 AND marriage_atx IS NOT NULL)
		AND proof IS NOT NULL
	)
	OR (pubkey = ?1 AND marriage_atx IS NULL AND proof IS NOT NULL);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}, nil)
	if err != nil {
		return false, fmt.Errorf("is malicious %v: %w", nodeID, err)
	}
	return rows > 0, nil
}

// GetBlobSizes returns the sizes of the blobs corresponding to malfeasance proofs for the
// specified identities. For non-existent proofs, the corresponding items are set to -1.
func GetBlobSizes(db sql.Executor, ids [][]byte) (sizes []int, err error) {
	return sql.GetBlobSizes(db, "select pubkey, length(proof) from identities where pubkey in", ids)
}

// LoadMalfeasanceBlob returns the malfeasance proof in raw bytes for the given identity.
func LoadMalfeasanceBlob(_ context.Context, db sql.Executor, nodeID []byte, blob *sql.Blob) error {
	err := sql.LoadBlob(db, "select proof from identities where pubkey = ?1;", nodeID, blob)
	if err == nil && len(blob.Bytes) == 0 {
		return sql.ErrNotFound
	}
	return err
}

// IterateMalicious invokes the specified callback for each malicious node ID.
// It stops if the callback returns an error.
func IterateMalicious(
	db sql.Executor,
	callback func(total int, id types.NodeID) error,
) error {
	var callbackErr error
	dec := func(stmt *sql.Statement) bool {
		var nid types.NodeID
		total := stmt.ColumnInt(0)
		stmt.ColumnBytes(1, nid[:])
		if callbackErr = callback(total, nid); callbackErr != nil {
			return false
		}
		return true
	}

	// Get total count in the same select statement to avoid the need for transaction
	if _, err := db.Exec(
		"select (select count(*) from identities where proof is not null) as total, "+
			"pubkey from identities where proof is not null", nil, dec); err != nil {
		return fmt.Errorf("get malicious identities: %w", err)
	}

	return callbackErr
}

// GetMalicious retrieves malicious node IDs from the database.
func GetMalicious(db sql.Executor) (ids []types.NodeID, err error) {
	if err = IterateMalicious(db, func(total int, nid types.NodeID) error {
		if ids == nil {
			ids = make([]types.NodeID, 0, total)
		}
		ids = append(ids, nid)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(ids) != cap(ids) {
		panic("BUG: bad malicious node ID count")
	}
	return ids, nil
}

// MarriageATX obtains the marriage ATX for given ID.
func MarriageATX(db sql.Executor, id types.NodeID) (types.ATXID, error) {
	var atx types.ATXID
	rows, err := db.Exec("SELECT marriage_atx FROM identities WHERE pubkey = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, func(stmt *sql.Statement) bool {
			if stmt.ColumnType(0) != sqlite.SQLITE_NULL {
				stmt.ColumnBytes(0, atx[:])
			}
			return false
		})
	if err != nil {
		return atx, fmt.Errorf("getting marriage ATX for %v: %w", id, err)
	}
	if rows == 0 {
		return atx, sql.ErrNotFound
	}
	return atx, nil
}

type MarriageData struct {
	ATX       types.ATXID
	Index     int
	Target    types.NodeID // ID that was married to
	Signature types.EdSignature
}

func Marriage(db sql.Executor, id types.NodeID) (*MarriageData, error) {
	var data MarriageData
	rows, err := db.Exec(`
	SELECT marriage_atx, marriage_idx, marriage_target, marriage_signature
	FROM identities
	WHERE pubkey = ?1 AND marriage_atx IS NOT NULL;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, func(stmt *sql.Statement) bool {
			stmt.ColumnBytes(0, data.ATX[:])
			data.Index = int(stmt.ColumnInt64(1))
			stmt.ColumnBytes(2, data.Target[:])
			stmt.ColumnBytes(3, data.Signature[:])
			return false
		})
	if err != nil {
		return nil, fmt.Errorf("marriage %v: %w", id, err)
	}
	if rows == 0 {
		return nil, sql.ErrNotFound
	}
	return &data, nil
}

// Set marriage inserts marriage data for given identity.
// If identity doesn't exist - create it.
func SetMarriage(db sql.Executor, id types.NodeID, m *MarriageData) error {
	isMalicious1, err := IsMalicious(db, id)
	if err != nil {
		return fmt.Errorf("checking if the node is malicious: %w", err)
	}
	isMalicious2, err := IsMalicious(db, m.Target)
	if err != nil {
		return fmt.Errorf("checking if the target is malicious: %w", err)
	}
	_, err = db.Exec(`
	INSERT INTO identities (pubkey, marriage_atx, marriage_idx, marriage_target, marriage_signature)
	values (?1, ?2, ?3, ?4, ?5)
	ON CONFLICT(pubkey) DO UPDATE SET
		marriage_atx = excluded.marriage_atx,
		marriage_idx = excluded.marriage_idx,
		marriage_target = excluded.marriage_target,
		marriage_signature = excluded.marriage_signature
	WHERE marriage_atx IS NULL;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
			stmt.BindBytes(2, m.ATX.Bytes())
			stmt.BindInt64(3, int64(m.Index))
			stmt.BindBytes(4, m.Target.Bytes())
			stmt.BindBytes(5, m.Signature.Bytes())
		}, nil,
	)
	if err != nil {
		return fmt.Errorf("setting marriage %v: %w", id, err)
	}
	if isMalicious1 || isMalicious2 {
		sql.AddToSet(db, "malicious", id[:])
		sql.AddToSet(db, "malicious", m.Target[:])
	}
	return nil
}

func IterateMarriages(db sql.Executor, cb func(id types.NodeID, data *MarriageData) bool) error {
	_, err := db.Exec(`
	SELECT pubkey, marriage_atx, marriage_idx, marriage_target, marriage_signature
	FROM identities
	WHERE marriage_atx IS NOT NULL;`,
		nil,
		func(stmt *sql.Statement) bool {
			var id types.NodeID
			var data MarriageData
			stmt.ColumnBytes(0, id[:])
			stmt.ColumnBytes(1, data.ATX[:])
			data.Index = int(stmt.ColumnInt64(2))
			stmt.ColumnBytes(3, data.Target[:])
			stmt.ColumnBytes(4, data.Signature[:])
			return cb(id, &data)
		},
	)
	return err
}

// EquivocationSet returns all node IDs that are married to the given node ID
// including itself.
func EquivocationSet(db sql.Executor, id types.NodeID) ([]types.NodeID, error) {
	var ids []types.NodeID

	rows, err := db.Exec(`
	SELECT pubkey FROM identities
	WHERE marriage_atx = (SELECT marriage_atx FROM identities WHERE pubkey = ?1) AND marriage_atx IS NOT NULL;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		},
		func(stmt *sql.Statement) bool {
			var nid types.NodeID
			stmt.ColumnBytes(0, nid[:])
			ids = append(ids, nid)
			return true
		})
	if err != nil {
		return nil, fmt.Errorf("getting marriage for %v: %w", id, err)
	}
	if rows == 0 {
		return []types.NodeID{id}, nil
	}

	return ids, nil
}

func EquivocationSetByMarriageATX(db sql.Executor, atx types.ATXID) ([]types.NodeID, error) {
	var ids []types.NodeID

	_, err := db.Exec(`
	SELECT pubkey FROM identities WHERE marriage_atx = ?1 ORDER BY marriage_idx ASC;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, atx.Bytes())
		},
		func(stmt *sql.Statement) bool {
			var nid types.NodeID
			stmt.ColumnBytes(0, nid[:])
			ids = append(ids, nid)
			return true
		})
	if err != nil {
		return nil, fmt.Errorf("getting equivocation set by ID %s: %w", atx, err)
	}

	return ids, nil
}
