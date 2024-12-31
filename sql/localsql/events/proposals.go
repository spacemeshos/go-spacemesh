package events

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func InsertProposal(db sql.Executor, proposal *types.Proposal) error {
	encoded, err := codec.Encode(proposal)
	if err != nil {
		return err
	}

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, proposal.SmesherID.Bytes())
		stmt.BindInt64(2, int64(proposal.Layer))
		stmt.BindBytes(3, encoded)
	}
	if _, err := db.Exec(
		`INSERT into proposals (id, layer, proposal) values (?1, ?2, ?3);`,
		enc,
		nil,
	); err != nil {
		return fmt.Errorf("inserting proposal for %s: %w", proposal.SmesherID.ShortString(), err)
	}
	return nil
}

func InterateAllProposals(
	db sql.Executor,
	fn func(p types.Proposal) bool,
) error {
	var stateBuf bytes.Buffer
	_, err := db.Exec(
		`SELECT proposal FROM proposals ORDER BY layer ASC`,
		nil,
		func(stmt *sql.Statement) bool {
			stateBuf.Reset()
			stateBuf.ReadFrom(stmt.ColumnReader(0))
			var proposal types.Proposal
			codec.MustDecode(stateBuf.Bytes(), &proposal)
			if err := proposal.Initialize(); err != nil {
				panic(fmt.Sprintf("initializing proposal: %v", err))
			}
			return fn(proposal)
		},
	)
	if err != nil {
		return fmt.Errorf("iterating proposals: %w", err)
	}
	return nil
}
