package nipost

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type PoETRegistration struct {
	NodeId        types.NodeID
	ChallengeHash types.Hash32
	Address       string
	RoundID       string
	RoundEnd      time.Time
}

func AddPoetRegistration(
	db sql.Executor,
	registration PoETRegistration,
) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, registration.NodeId.Bytes())
		stmt.BindBytes(2, registration.ChallengeHash.Bytes())
		stmt.BindText(3, registration.Address)
		stmt.BindText(4, registration.RoundID)
		stmt.BindInt64(5, registration.RoundEnd.Unix())
	}
	if _, err := db.Exec(`
	insert into poet_registration (id, hash, address, round_id, round_end) 
		values (?1, ?2, ?3, ?4, ?5)
		on conflict (id, address) 
		do update set round_id = excluded.round_id, round_end = excluded.round_end;`, enc, nil,
	); err != nil {
		return fmt.Errorf("insert poet registration for %s: %w", registration.NodeId, err)
	}
	return nil
}

func ClearPoetRegistrations(db sql.Executor, nodeID types.NodeID) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	if _, err := db.Exec(`delete from poet_registration where id = ?1;`, enc, nil); err != nil {
		return fmt.Errorf("clear poet registrations for %s: %w", nodeID.ShortString(), err)
	}
	return nil
}

func PoetRegistrations(db sql.Executor, nodeID types.NodeID) ([]PoETRegistration, error) {
	var registrations []PoETRegistration

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}

	dec := func(stmt *sql.Statement) bool {
		registration := PoETRegistration{
			Address:  stmt.ColumnText(1),
			RoundID:  stmt.ColumnText(2),
			RoundEnd: time.Unix(stmt.ColumnInt64(3), 0),
		}
		stmt.ColumnBytes(0, registration.ChallengeHash[:])
		registrations = append(registrations, registration)
		return true
	}

	query := `SELECT hash, address, round_id, round_end FROM poet_registration WHERE id = ?1;`

	_, err := db.Exec(query, enc, dec)
	if err != nil {
		return nil, fmt.Errorf("get poet registrations for node id %s: %w", nodeID.ShortString(), err)
	}

	return registrations, nil
}
