package poets

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/sql"
)

// GetPoET gets a PoET for a given ref.
func GetPoET(db sql.Executor, ref []byte) (poet []byte, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, ref)
	}
	dec := func(stmt *sql.Statement) bool {
		poet = make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, poet[:])
		return true
	}

	rows, err := db.Exec("select poet from poets where ref = ?1;", enc, dec)
	if err != nil {
		return nil, fmt.Errorf("get value: %w", err)
	}
	if rows == 0 {
		return nil, fmt.Errorf("get value: %w", sql.ErrNotFound)
	}

	return poet, nil
}

// AddPoET adds a poet for a given ref.
func AddPoET(db sql.Executor, ref, poet []byte) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, ref)
		stmt.BindBytes(2, poet)
	}
	_, err := db.Exec("insert into poets (ref, poet) values (?1, ?2);", enc, nil)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

// GetRef gets a PoET ref for a given service ID and round ID.
func GetRef(db sql.Executor, poetID []byte, roundID string) (ref []byte, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, poetID)
		stmt.BindBytes(2, []byte(roundID))
	}
	dec := func(stmt *sql.Statement) bool {
		ref = make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, ref[:])
		return true
	}

	rows, err := db.Exec(`
		select ref from poet_subscriptions 
		where service_id = ?1 and round_id = ?2;`, enc, dec)
	if err != nil {
		return nil, fmt.Errorf("get value: %w", err)
	}
	if rows == 0 {
		return nil, fmt.Errorf("get value: %w", sql.ErrNotFound)
	}

	return ref, nil
}

// AddRef adds a poet ref for service ID and round ID.
func AddRef(db sql.Executor, serviceID []byte, roundID string, ref []byte) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, serviceID)
		stmt.BindBytes(2, []byte(roundID))
		stmt.BindBytes(3, ref)
	}
	_, err := db.Exec(`
		insert into poet_subscriptions (service_id, round_id, ref) 
		values (?1, ?2, ?3);`, enc, nil)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

// DeleteRef removes PoET ref for given service ID and round ID.
func DeleteRef(db sql.Executor, serviceID []byte, roundID string) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, serviceID)
		stmt.BindBytes(2, []byte(roundID))
	}
	_, err := db.Exec("delete from poet_subscriptions where service_id = ?1 and round_id = ?2;", enc, nil)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	return nil
}

// GetBlob loads PoET as an encoded blob, ready to be sent over the wire.
func GetBlob(db sql.Executor, ref []byte) (poet []byte, err error) {
	return GetPoET(db, ref)
}
