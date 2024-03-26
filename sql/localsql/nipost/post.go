package nipost

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type Post struct {
	Nonce     uint32
	Indices   []byte
	Pow       uint64
	Challenge []byte

	NumUnits      uint32
	CommitmentATX types.ATXID
	VRFNonce      types.VRFPostIndex
}

func AddPost(db sql.Executor, nodeID types.NodeID, post Post) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(post.Nonce))
		stmt.BindBytes(3, post.Indices)
		stmt.BindInt64(4, int64(post.Pow))
		stmt.BindBytes(5, post.Challenge)
		stmt.BindInt64(6, int64(post.NumUnits))
		stmt.BindBytes(7, post.CommitmentATX.Bytes())
		stmt.BindInt64(8, int64(post.VRFNonce))
	}
	if _, err := db.Exec(`
		INSERT into post (
			id, post_nonce, post_indices, post_pow, challenge, num_units, commit_atx, vrf_nonce
		) values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8);`, enc, nil,
	); err != nil {
		return fmt.Errorf("inserting post for %s: %w", nodeID.ShortString(), err)
	}
	return nil
}

func RemovePost(db sql.Executor, nodeID types.NodeID) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	if _, err := db.Exec(`
		delete from post where id = ?1;`, enc, nil,
	); err != nil {
		return fmt.Errorf("delete post for %s: %w", nodeID, err)
	}
	return nil
}

func GetPost(db sql.Executor, nodeID types.NodeID) (*Post, error) {
	var post *Post
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		post = &Post{
			Nonce:     uint32(stmt.ColumnInt64(0)),
			Indices:   make([]byte, stmt.ColumnLen(1)),
			Pow:       uint64(stmt.ColumnInt64(2)),
			Challenge: make([]byte, stmt.ColumnLen(3)),

			NumUnits: uint32(stmt.ColumnInt64(4)),
			VRFNonce: types.VRFPostIndex(stmt.ColumnInt64(6)),
		}
		stmt.ColumnBytes(1, post.Indices)
		stmt.ColumnBytes(3, post.Challenge)
		stmt.ColumnBytes(5, post.CommitmentATX[:])
		return true
	}
	if _, err := db.Exec(`
	select post_nonce, post_indices, post_pow, challenge, num_units, commit_atx, vrf_nonce
	from post where id = ?1 limit 1;`, enc, dec,
	); err != nil {
		return nil, fmt.Errorf("getting post for node id %s: %w", nodeID.ShortString(), err)
	}
	if post == nil {
		return nil, fmt.Errorf("getting post for node id %s: %w", nodeID.ShortString(), sql.ErrNotFound)
	}
	return post, nil
}
