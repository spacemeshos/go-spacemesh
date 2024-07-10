package nipost

import (
	"fmt"
	"strings"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type PoETRegistration struct {
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

func PoetRegistrationCount(db sql.Executor, nodeID types.NodeID, addresses ...string) (int, error) {
	var (
		count int
		enc   func(stmt *sql.Statement)
		query string
	)

	if len(addresses) > 0 {
		enc = func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
			for i, addr := range addresses {
				stmt.BindText(i+2, addr) // Start binding addresses at position 2
			}
		}

		placeholders := make([]string, len(addresses))
		for i := range placeholders {
			placeholders[i] = fmt.Sprintf("?%d", i+2) // Generate placeholders starting from ?2
		}
		// Construct the query with IN clause for addresses
		query = fmt.Sprintf(`select count(*) from poet_registration where id = ?1 and address IN (%s);`,
			strings.Join(placeholders, ", "))

	} else {
		enc = func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}

		query = `select count(*) from poet_registration where id = ?1;`
	}

	dec := func(stmt *sql.Statement) bool {
		count = int(stmt.ColumnInt64(0))
		return true
	}

	_, err := db.Exec(query, enc, dec)
	if err != nil {
		if len(addresses) > 0 {
			return 0, fmt.Errorf("get poet registration count for node id %s and addresses %v: %w",
				nodeID.ShortString(), addresses, err)
		}
		return 0, fmt.Errorf("get poet registration count for node id %s: %w", nodeID.ShortString(), err)
	}
	return count, nil
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

func PoetRegistrations(db sql.Executor, nodeID types.NodeID, addresses ...string) ([]PoETRegistration, error) {
	var registrations []PoETRegistration

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		if len(addresses) > 0 {
			for i, address := range addresses {
				stmt.BindText(i+2, address)
			}
		}
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

	var query string
	if len(addresses) > 0 {
		placeholders := make([]string, len(addresses))
		for i := range addresses {
			placeholders[i] = fmt.Sprintf("?%d", i+2)
		}
		query = fmt.Sprintf(`
            SELECT hash, address, round_id, round_end 
            FROM poet_registration 
            WHERE id = ?1 AND address IN (%s);`,
			strings.Join(placeholders, ", "))
	} else {
		query = `
            SELECT hash, address, round_id, round_end 
            FROM poet_registration 
            WHERE id = ?1;`
	}

	_, err := db.Exec(query, enc, dec)
	if err != nil {
		return nil, fmt.Errorf("get poet registrations for node id %s: %w", nodeID.ShortString(), err)
	}

	return registrations, nil
}
