package states

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func InsertEligibility(
	db sql.Executor,
	id types.NodeID,
	layer types.LayerID,
	eligibility *types.VotingEligibility,
) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindInt64(2, int64(layer))
		stmt.BindInt64(3, int64(eligibility.J))
		stmt.BindBytes(4, eligibility.Sig.Bytes())
	}
	if _, err := db.Exec(
		`INSERT into state_proposals (id, layer, j, signature) values (?1, ?2, ?3, ?4);`,
		enc,
		nil,
	); err != nil {
		return fmt.Errorf("inserting state for %s: %w", id.ShortString(), err)
	}
	return nil
}

func InterateAllEligibilities(
	db sql.Executor,
	fn func(id types.NodeID, layer types.LayerID, eligibility *types.VotingEligibility) bool,
) error {
	_, err := db.Exec(
		`SELECT id, layer, j, signature FROM state_eligibilities`,
		nil,
		func(stmt *sql.Statement) bool {
			var (
				id          types.NodeID
				eligibility types.VotingEligibility
			)
			stmt.ColumnBytes(0, id[:])
			layer := types.LayerID(stmt.ColumnInt64(1))
			eligibility.J = uint32(stmt.ColumnInt64(2))
			stmt.ColumnBytes(3, eligibility.Sig[:])
			return fn(id, layer, &eligibility)
		},
	)
	if err != nil {
		return fmt.Errorf("iterate atx fields: %w", err)
	}
	return nil
}
