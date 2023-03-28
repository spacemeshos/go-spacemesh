package proposals

import (
	"fmt"
	"io"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Get gets a proposal by a given ID.
func Get(db sql.Executor, id types.ProposalID) (proposal *types.Proposal, err error) {
	if rows, err := db.Exec(`
		select 
			ballots.pubkey, 
			ballots.ballot, 
			length(identities.proof),
			proposals.id, 
			proposals.ballot_id, 
			proposals.tx_ids, 
			proposals.mesh_hash,
			proposals.signature
		from proposals 
		left join ballots on proposals.ballot_id = ballots.id 
		left join identities using(pubkey)
		where proposals.id = ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, func(stmt *sql.Statement) bool {
			proposal, err = decodeProposal(stmt)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w proposal ID %s", sql.ErrNotFound, id)
	}

	return proposal, err
}

// Has checks if a proposal exists by a given ID.
func Has(db sql.Executor, id types.ProposalID) (bool, error) {
	rows, err := db.Exec("select 1 from proposals where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, nil,
	)
	if err != nil {
		return false, fmt.Errorf("exec id %v: %w", id, err)
	}
	return rows > 0, nil
}

// GetByLayer gets proposals by a given layer ID.
func GetByLayer(db sql.Executor, layerID types.LayerID) (proposals []*types.Proposal, err error) {
	if rows, err := db.Exec(`
		select 
			ballots.pubkey, 
			ballots.ballot, 
			length(identities.proof),
			proposals.id, 
			proposals.ballot_id, 
			proposals.tx_ids, 
			proposals.mesh_hash,
			proposals.signature 
		from proposals 
		left join ballots on proposals.ballot_id = ballots.id 
		left join identities using(pubkey)
		where proposals.layer = ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(layerID.Uint32()))
		}, func(stmt *sql.Statement) bool {
			proposal, decodeErr := decodeProposal(stmt)
			if decodeErr != nil {
				err = decodeErr
				return true
			}

			proposals = append(proposals, proposal)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get %s: %w", layerID, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w layer %s", sql.ErrNotFound, layerID)
	}

	return proposals, err
}

// GetBlob loads proposal as an encoded blob, ready to be sent over the wire.
func GetBlob(db sql.Executor, id []byte) (proposal []byte, err error) {
	if rows, err := db.Exec(`select proposal from proposals where id = ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id)
		}, func(stmt *sql.Statement) bool {
			proposal = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, proposal)
			return true
		}); err != nil {
		return nil, fmt.Errorf("exec %s: %w", types.BytesToHash(id), err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w proposal ID %s", sql.ErrNotFound, types.BytesToHash(id))
	}

	return proposal, err
}

// Add adds a proposal for a given ID.
func Add(db sql.Executor, proposal *types.Proposal) error {
	txIDsBytes, err := codec.EncodeSlice(proposal.TxIDs)
	if err != nil {
		return fmt.Errorf("encode TX IDs: %w", err)
	}
	encodedProposal, err := codec.Encode(proposal)
	if err != nil {
		return fmt.Errorf("encode proposal: %w", err)
	}

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, proposal.ID().Bytes())
		stmt.BindBytes(2, proposal.Ballot.ID().Bytes())
		stmt.BindInt64(3, int64(proposal.Layer.Uint32()))
		stmt.BindBytes(4, txIDsBytes)
		stmt.BindBytes(5, proposal.MeshHash.Bytes())
		stmt.BindBytes(6, proposal.Signature.Bytes())
		stmt.BindBytes(7, encodedProposal)
	}

	_, err = db.Exec(`
		insert into proposals (id, ballot_id, layer, tx_ids, mesh_hash, signature, proposal)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7);`, enc, nil)
	if err != nil {
		return fmt.Errorf("insert proposal ID %v: %w", proposal.ID(), err)
	}

	return nil
}

func decodeProposal(stmt *sql.Statement) (*types.Proposal, error) {
	ballotID := types.BallotID{}
	stmt.ColumnBytes(4, ballotID[:])

	var nodeID types.NodeID
	stmt.ColumnBytes(0, nodeID[:])

	bodyBytes := make([]byte, stmt.ColumnLen(1))
	stmt.ColumnBytes(1, bodyBytes[:])

	ballot := types.Ballot{}
	if err := codec.Decode(bodyBytes, &ballot); err != nil {
		return nil, err
	}
	ballot.SetID(ballotID)
	ballot.SetSmesherID(nodeID)
	if stmt.ColumnInt(2) > 0 {
		ballot.SetMalicious()
	}

	proposalID := types.ProposalID{}
	stmt.ColumnBytes(3, proposalID[:])

	txIDsBytes := make([]byte, stmt.ColumnLen(5))
	stmt.ColumnBytes(5, txIDsBytes)

	meshBytes := make([]byte, stmt.ColumnLen(6))
	stmt.ColumnBytes(6, meshBytes)
	signature := types.EdSignature{}
	stmt.ColumnBytes(7, signature[:])

	txIDs, err := codec.DecodeSlice[types.TransactionID](txIDsBytes)
	if err != nil {
		if err != io.EOF {
			return nil, fmt.Errorf("decode TX IDs: %w", err)
		}
	}
	proposal := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot:   ballot,
			TxIDs:    txIDs,
			MeshHash: types.BytesToHash(meshBytes),
		},
		Signature: signature,
	}
	proposal.SetID(proposalID)
	return proposal, nil
}
