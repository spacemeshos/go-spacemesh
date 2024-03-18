package poets

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Has checks if a PoET exists by the given ref.
func Has(db sql.Executor, ref types.PoetProofRef) (bool, error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, ref[:])
	}
	rows, err := db.Exec("select 1 from poets where ref = ?1;", enc, nil)
	if err != nil {
		return false, fmt.Errorf("has: %w", err)
	}
	return rows > 0, nil
}

// GetBlobSizes returns the sizes of the blobs corresponding to PoETs with specified
// refs. For non-existent PoETs, the corresponding items are set to -1.
func GetBlobSizes(db sql.Executor, refs [][]byte) (sizes []int, err error) {
	return sql.GetBlobSizes(db, "select ref, length(poet) from poets where ref in", refs)
}

// LoadBlob loads PoET as an encoded blob, ready to be sent over the wire.
func LoadBlob(ctx context.Context, db sql.Executor, ref []byte, blob *sql.Blob) error {
	return sql.LoadBlob(db, "select poet from poets where ref = ?1", ref, blob)
}

// Get gets a PoET for a given ref.
func Get(db sql.Executor, ref types.PoetProofRef) (poet []byte, err error) {
	var b sql.Blob
	if err := LoadBlob(context.Background(), db, ref[:], &b); err != nil {
		return nil, err
	}
	return b.Bytes, nil
}

// Add adds a poet for a given ref.
func Add(db sql.Executor, ref types.PoetProofRef, poet, serviceID []byte, roundID string) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, ref[:])
		stmt.BindBytes(2, poet)
		stmt.BindBytes(3, serviceID)
		stmt.BindBytes(4, []byte(roundID))
	}
	_, err := db.Exec(`
		insert into poets (ref, poet, service_id, round_id)
		values (?1, ?2, ?3, ?4);`, enc, nil)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

// GetRef gets a PoET ref for a given service ID and round ID.
func GetRef(db sql.Executor, poetID []byte, roundID string) (ref types.PoetProofRef, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, poetID)
		stmt.BindBytes(2, []byte(roundID))
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, ref[:])
		return true
	}

	rows, err := db.Exec(`
		select ref from poets
		where service_id = ?1 and round_id = ?2;`, enc, dec)
	if err != nil {
		return types.PoetProofRef{}, fmt.Errorf("get value: %w", err)
	}
	if rows == 0 {
		return types.PoetProofRef{}, fmt.Errorf("get value: %w", sql.ErrNotFound)
	}

	return ref, nil
}
