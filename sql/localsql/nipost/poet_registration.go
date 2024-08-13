package nipost

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var ErrNoNodeId = errors.New("np node id is given")

type PoETRegistration struct {
	NodeId        types.NodeID
	ChallengeHash types.Hash32
	Address       string
	RoundID       string
	RoundEnd      time.Time
}

func AddPoetRegistration(
	db sql.Executor,
	nodeID types.NodeID,
	registration PoETRegistration,
) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, registration.ChallengeHash.Bytes())
		stmt.BindText(3, registration.Address)
		stmt.BindText(4, registration.RoundID)
		stmt.BindInt64(5, registration.RoundEnd.Unix())
	}
	if _, err := db.Exec(`
		insert into poet_registration (id, hash, address, round_id, round_end)
		values (?1, ?2, ?3, ?4, ?5);`, enc, nil,
	); err != nil {
		return fmt.Errorf("insert poet registration for %s: %w", nodeID, err)
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

func PoetRegistrations(db sql.Executor, nodeIDs ...types.NodeID) ([]PoETRegistration, error) {
	if len(nodeIDs) == 0 {
		return nil, ErrNoNodeId
	}

	var registrations []PoETRegistration

	enc := func(stmt *sql.Statement) {
		for i, nodeID := range nodeIDs {
			stmt.BindBytes(i+1, nodeID.Bytes())
		}
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

	placeholders := make([]string, len(nodeIDs))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("?%d", i+1)
	}
	placeholderStr := strings.Join(placeholders, ", ")

	query := fmt.Sprintf(`SELECT id, hash, address, round_id, round_end FROM poet_registration WHERE id IN (%s);`, placeholderStr)

	_, err := db.Exec(query, enc, dec)
	if err != nil {
		nodeIDStrings := make([]string, len(nodeIDs))
		for i, nodeID := range nodeIDs {
			nodeIDStrings[i] = nodeID.ShortString()
		}
		return nil, fmt.Errorf("get poet registrations for node ids %s: %w", strings.Join(nodeIDStrings, ", "), err)
	}

	return registrations, nil
}
