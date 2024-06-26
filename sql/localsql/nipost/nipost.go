package nipost

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type NIPostState struct {
	*types.NIPost

	NumUnits uint32
	VRFNonce types.VRFPostIndex
}

func AddNIPost(db sql.Executor, nodeID types.NodeID, nipost *NIPostState) error {
	buf, err := codec.Encode(&nipost.Membership)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(nipost.Post.Nonce))
		stmt.BindBytes(3, nipost.Post.Indices)
		stmt.BindInt64(4, int64(nipost.Post.Pow))

		stmt.BindInt64(5, int64(nipost.NumUnits))
		stmt.BindInt64(6, int64(nipost.VRFNonce))

		stmt.BindBytes(7, buf)

		stmt.BindBytes(8, nipost.PostMetadata.Challenge)
		stmt.BindInt64(9, int64(nipost.PostMetadata.LabelsPerUnit))
	}

	if _, err := db.Exec(`
		insert into nipost (id, post_nonce, post_indices, post_pow, num_units, vrf_nonce,
			 poet_proof_membership, poet_proof_ref, labels_per_unit
		) values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9);`, enc, nil,
	); err != nil {
		return fmt.Errorf("insert nipost for %s: %w", nodeID, err)
	}
	return nil
}

func RemoveNIPost(db sql.Executor, nodeID types.NodeID) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	if _, err := db.Exec(`
		delete from nipost where id = ?1;`, enc, nil,
	); err != nil {
		return fmt.Errorf("delete nipost for %s: %w", nodeID, err)
	}
	return nil
}

func NIPost(db sql.Executor, nodeID types.NodeID) (*NIPostState, error) {
	var nipost *NIPostState
	var decodeErr error
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		nipost = &NIPostState{
			NIPost: &types.NIPost{},
		}

		n := stmt.ColumnLen(1)
		nipost.Post = &types.Post{
			Nonce:   uint32(stmt.ColumnInt64(0)),
			Indices: make([]byte, n),
			Pow:     uint64(stmt.ColumnInt64(2)),
		}
		stmt.ColumnBytes(1, nipost.Post.Indices)

		nipost.NumUnits = uint32(stmt.ColumnInt64(3))
		nipost.VRFNonce = types.VRFPostIndex(stmt.ColumnInt64(4))

		_, decodeErr = codec.DecodeFrom(stmt.ColumnReader(5), &nipost.Membership)

		nipost.PostMetadata = &types.PostMetadata{
			Challenge:     make([]byte, stmt.ColumnLen(6)),
			LabelsPerUnit: uint64(stmt.ColumnInt64(7)),
		}
		stmt.ColumnBytes(6, nipost.PostMetadata.Challenge)
		return true
	}
	if _, err := db.Exec(`
		select post_nonce, post_indices, post_pow, num_units, vrf_nonce,
			poet_proof_membership, poet_proof_ref, labels_per_unit
		from nipost where id = ?1 limit 1;`, enc, dec,
	); err != nil {
		return nil, fmt.Errorf("get nipost from node id %s: %w", nodeID.ShortString(), err)
	}
	if nipost == nil {
		return nil, fmt.Errorf("get nipost from node id %s: %w", nodeID.ShortString(), sql.ErrNotFound)
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("decode nipost membership for node id %s: %w", nodeID.ShortString(), decodeErr)
	}
	return nipost, nil
}
