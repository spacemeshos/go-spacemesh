package nipost

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type Post struct {
	Nonce   uint32
	Indices []byte
	Pow     uint64

	NumUnits      uint32
	CommitmentATX types.ATXID
	VRFNonce      types.VRFPostIndex
}

func AddInitialPost(db sql.Executor, nodeID types.NodeID, post Post) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(post.Nonce))
		stmt.BindBytes(3, post.Indices)
		stmt.BindInt64(4, int64(post.Pow))
		stmt.BindInt64(5, int64(post.NumUnits))
		stmt.BindBytes(6, post.CommitmentATX.Bytes())
		stmt.BindInt64(7, int64(post.VRFNonce))
	}
	if _, err := db.Exec(`
		insert into initial_post (id, post_nonce, post_indices, post_pow, num_units, commit_atx, vrf_nonce)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7);`, enc, nil,
	); err != nil {
		return fmt.Errorf("insert initial post for %s: %w", nodeID.ShortString(), err)
	}
	return nil
}

func RemoveInitialPost(db sql.Executor, nodeID types.NodeID) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	if _, err := db.Exec(`delete from initial_post where id = ?1;`, enc, nil); err != nil {
		return fmt.Errorf("remove initial post for %s: %w", nodeID, err)
	}
	return nil
}

func InitialPost(db sql.Executor, nodeID types.NodeID) (*Post, error) {
	var post *Post
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		post = &Post{
			Nonce:   uint32(stmt.ColumnInt64(0)),
			Indices: make([]byte, stmt.ColumnLen(1)),
			Pow:     uint64(stmt.ColumnInt64(2)),

			NumUnits: uint32(stmt.ColumnInt64(3)),
			VRFNonce: types.VRFPostIndex(stmt.ColumnInt64(5)),
		}
		stmt.ColumnBytes(1, post.Indices)
		stmt.ColumnBytes(4, post.CommitmentATX[:])
		return true
	}
	if _, err := db.Exec(`
		select post_nonce, post_indices, post_pow, num_units, commit_atx, vrf_nonce
		from initial_post where id = ?1 limit 1;`, enc, dec,
	); err != nil {
		return nil, fmt.Errorf("get initial post from node id %s: %w", nodeID.ShortString(), err)
	}
	if post == nil {
		return nil, fmt.Errorf("get initial post from node id %s: %w", nodeID.ShortString(), sql.ErrNotFound)
	}
	return post, nil
}

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
		insert into nipost (id, epoch, sequence, prev_atx, pos_atx, commit_atx,
			post_nonce, post_indices, post_pow)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9);`, encodeNipostChallenge(nodeID, ch), nil,
	); err != nil {
		return fmt.Errorf("insert nipost challenge for %s pub-epoch %d: %w", nodeID, ch.PublishEpoch, err)
	}
	return nil
}

func UpdateChallenge(db sql.Executor, nodeID types.NodeID, ch *types.NIPostChallenge) error {
	if _, err := db.Exec(`
		update nipost set epoch = ?2, sequence = ?3, prev_atx = ?4, pos_atx = ?5,
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
	if _, err := db.Exec(`delete from nipost where id = ?1;`, enc, nil); err != nil {
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
		from nipost where id = ?1 limit 1;`, enc, dec,
	); err != nil {
		return nil, fmt.Errorf("get challenge from node id %s: %w", nodeID.ShortString(), err)
	}
	if ch == nil {
		return nil, fmt.Errorf("get challenge from node id %s: %w", nodeID.ShortString(), sql.ErrNotFound)
	}
	return ch, nil
}
