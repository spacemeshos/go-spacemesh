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
		values (?1, ?2, ?3, ?4, ?5);`, enc, nil,
	); err != nil {
		return fmt.Errorf("insert poet registration for %s: %w", registration.NodeId, err)
	}
	return nil
}

func UpdatePoetRegistration(db sql.Executor, registration PoETRegistration) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindText(1, registration.RoundID)
		stmt.BindInt64(2, registration.RoundEnd.Unix())
		stmt.BindBytes(3, registration.NodeId.Bytes())
		stmt.BindText(4, registration.Address)
		stmt.BindBytes(5, registration.ChallengeHash.Bytes())
	}

	query := `
        update poet_registration 
        SET round_id = ?1, round_end = ?2
        where id = ?3 AND address = ?4 AND hash = ?5;`

	_, err := db.Exec(query, enc, nil)
	if err != nil {
		return fmt.Errorf("update poet registration for %s: %w", registration.NodeId.String(), err)
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
			Address:  stmt.ColumnText(2),
			RoundID:  stmt.ColumnText(3),
			RoundEnd: time.Unix(stmt.ColumnInt64(4), 0),
		}

		nodeId := make([]byte, types.NodeIDSize)
		stmt.ColumnBytes(0, nodeId)

		registration.NodeId = types.BytesToNodeID(nodeId)

		stmt.ColumnBytes(1, registration.ChallengeHash[:])
		registrations = append(registrations, registration)
		return true
	}

	query := `SELECT id, hash, address, round_id, round_end FROM poet_registration WHERE id = ?1;`

	_, err := db.Exec(query, enc, dec)
	if err != nil {
		return nil, fmt.Errorf("get poet registrations for node id %s: %w", nodeID.ShortString(), err)
	}

	return registrations, nil
}
