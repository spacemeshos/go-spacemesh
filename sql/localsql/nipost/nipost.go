package nipost

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func AddNIPost(db sql.Executor, nodeID types.NodeID, nipost *types.NIPost) error {
	buf, err := codec.Encode(&nipost.Membership)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(nipost.Post.Nonce))
		stmt.BindBytes(3, nipost.Post.Indices)
		stmt.BindInt64(4, int64(nipost.Post.Pow))

		stmt.BindBytes(5, buf)

		stmt.BindBytes(6, nipost.PostMetadata.Challenge)
		stmt.BindInt64(7, int64(nipost.PostMetadata.LabelsPerUnit))
	}

	if _, err := db.Exec(`
		insert into nipost (id, post_nonce, post_indices, post_pow, poet_proof_membership, poet_proof_ref, labels_per_unit)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7);`, enc, nil,
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

func NIPost(db sql.Executor, nodeID types.NodeID) (*types.NIPost, error) {
	var nipost *types.NIPost
	var decodeErr error
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		nipost = &types.NIPost{}

		n := stmt.ColumnLen(1)
		nipost.Post = &types.Post{
			Nonce:   uint32(stmt.ColumnInt64(0)),
			Indices: make([]byte, n),
			Pow:     uint64(stmt.ColumnInt64(2)),
		}
		stmt.ColumnBytes(1, nipost.Post.Indices)

		nipost.Membership = types.MerkleProof{}
		_, decodeErr = codec.DecodeFrom(stmt.ColumnReader(3), &nipost.Membership)

		nipost.PostMetadata = &types.PostMetadata{
			Challenge:     make([]byte, stmt.ColumnLen(4)),
			LabelsPerUnit: uint64(stmt.ColumnInt64(5)),
		}
		stmt.ColumnBytes(4, nipost.PostMetadata.Challenge)
		return true
	}
	if _, err := db.Exec(`
		select post_nonce, post_indices, post_pow, poet_proof_membership, poet_proof_ref, labels_per_unit
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
