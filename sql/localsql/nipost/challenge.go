package nipost

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func encodeNipostChallenge(nodeID types.NodeID, ch *types.NIPostChallenge) func(stmt *sql.Statement) {
	return func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(ch.PublishEpoch))
		stmt.BindInt64(3, int64(ch.Sequence))
		stmt.BindBytes(4, ch.PrevATXID.Bytes())
		stmt.BindBytes(5, ch.PositioningATX.Bytes())
		if ch.CommitmentATX != nil {
			stmt.BindBytes(6, ch.CommitmentATX.Bytes())
		} else {
			stmt.BindNull(6)
		}
		if ch.InitialPost != nil {
			stmt.BindInt64(7, int64(ch.InitialPost.Nonce))
			stmt.BindBytes(8, ch.InitialPost.Indices)
			stmt.BindInt64(9, int64(ch.InitialPost.Pow))
		} else {
			stmt.BindNull(7)
			stmt.BindNull(8)
			stmt.BindNull(9)
		}
	}
}

func AddChallenge(db sql.Executor, nodeID types.NodeID, ch *types.NIPostChallenge) error {
	if _, err := db.Exec(`
		insert into challenge (id, epoch, sequence, prev_atx, pos_atx, commit_atx,
			post_nonce, post_indices, post_pow)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9);`, encodeNipostChallenge(nodeID, ch), nil,
	); err != nil {
		return fmt.Errorf("insert nipost challenge for %s pub-epoch %d: %w", nodeID, ch.PublishEpoch, err)
	}
	return nil
}

func UpdateChallenge(db sql.Executor, nodeID types.NodeID, ch *types.NIPostChallenge) error {
	if _, err := db.Exec(`
		update challenge set epoch = ?2, sequence = ?3, prev_atx = ?4, pos_atx = ?5,
			commit_atx = ?6, post_nonce = ?7, post_indices = ?8, post_pow = ?9
		where id = ?1;`, encodeNipostChallenge(nodeID, ch), nil,
	); err != nil {
		return fmt.Errorf("update nipost challenge for %s pub-epoch %d: %w", nodeID, ch.PublishEpoch, err)
	}
	return nil
}

func RemoveChallenge(db sql.Executor, nodeID types.NodeID) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	if _, err := db.Exec(`delete from challenge where id = ?1;`, enc, nil); err != nil {
		return fmt.Errorf("remove nipost challenge for %s: %w", nodeID, err)
	}
	return nil
}

func Challenge(db sql.Executor, nodeID types.NodeID) (*types.NIPostChallenge, error) {
	var ch *types.NIPostChallenge
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		ch = &types.NIPostChallenge{}
		ch.PublishEpoch = types.EpochID(stmt.ColumnInt64(0))
		ch.Sequence = uint64(stmt.ColumnInt64(1))
		stmt.ColumnBytes(2, ch.PrevATXID[:])
		stmt.ColumnBytes(3, ch.PositioningATX[:])
		ch.CommitmentATX = &types.ATXID{}
		if n := stmt.ColumnBytes(4, ch.CommitmentATX[:]); n == 0 {
			ch.CommitmentATX = nil
		}
		if n := stmt.ColumnLen(6); n > 0 {
			ch.InitialPost = &types.Post{
				Nonce:   uint32(stmt.ColumnInt64(5)),
				Indices: make([]byte, n),
				Pow:     uint64(stmt.ColumnInt64(7)),
			}
			stmt.ColumnBytes(6, ch.InitialPost.Indices)
		}
		return true
	}
	if _, err := db.Exec(`
		select epoch, sequence, prev_atx, pos_atx, commit_atx,
			post_nonce, post_indices, post_pow
		from challenge where id = ?1 limit 1;`, enc, dec,
	); err != nil {
		return nil, fmt.Errorf("get challenge from node id %s: %w", nodeID.ShortString(), err)
	}
	if ch == nil {
		return nil, fmt.Errorf("get challenge from node id %s: %w", nodeID.ShortString(), sql.ErrNotFound)
	}
	return ch, nil
}

func UpdatePoetProofRef(
	db sql.Executor,
	nodeID types.NodeID,
	ref types.PoetProofRef,
	membership *types.MerkleProof,
) error {
	buf, err := codec.Encode(membership)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, ref[:])
		stmt.BindBytes(3, buf)
	}
	rows, err := db.Exec(`
		update challenge set poet_proof_ref = ?2, poet_proof_membership = ?3
		where id = ?1 returning id;`, enc, nil)
	if err != nil {
		return fmt.Errorf("set poet proof ref for node id %s: %w", nodeID.ShortString(), err)
	}
	if rows == 0 {
		return fmt.Errorf("set poet proof ref for node id %s: %w", nodeID.ShortString(), sql.ErrNotFound)
	}
	return nil
}

func PoetProofRef(db sql.Executor, nodeID types.NodeID) (types.PoetProofRef, *types.MerkleProof, error) {
	var ref types.PoetProofRef
	var membership *types.MerkleProof
	var decodeErr error
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, ref[:])
		if stmt.ColumnLen(1) > 0 {
			membership = &types.MerkleProof{}
			_, decodeErr = codec.DecodeFrom(stmt.ColumnReader(1), membership)
		}
		return true
	}
	_, err := db.Exec(`select poet_proof_ref, poet_proof_membership from challenge where id = ?1 limit 1;`, enc, dec)
	if err != nil {
		return types.PoetProofRef{}, nil, fmt.Errorf("get poet proof ref from node id %s: %w",
			nodeID.ShortString(), err,
		)
	}
	if membership == nil {
		return types.PoetProofRef{}, nil,
			fmt.Errorf("get poet proof ref from node id %s: %w", nodeID.ShortString(), sql.ErrNotFound)
	}
	if decodeErr != nil {
		return types.PoetProofRef{}, nil,
			fmt.Errorf("decode proof membership for node id %s: %w", nodeID.ShortString(), decodeErr)
	}
	return ref, membership, nil
}
