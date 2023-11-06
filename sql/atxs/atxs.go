package atxs

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const fullQuery = `select id, atx, base_tick_height, tick_count, pubkey,
	effective_num_units, received, epoch, sequence, coinbase from atxs`

type decoderCallback func(*types.VerifiedActivationTx, error) bool

func decoder(fn decoderCallback) sql.Decoder {
	return func(stmt *sql.Statement) bool {
		var (
			a  types.ActivationTx
			id types.ATXID
		)
		stmt.ColumnBytes(0, id[:])
		checkpointed := stmt.ColumnLen(1) == 0
		if !checkpointed {
			if _, err := codec.DecodeFrom(stmt.ColumnReader(1), &a); err != nil {
				return fn(nil, fmt.Errorf("decode %w", err))
			}
		}
		a.SetID(id)
		baseTickHeight := uint64(stmt.ColumnInt64(2))
		tickCount := uint64(stmt.ColumnInt64(3))
		stmt.ColumnBytes(4, a.SmesherID[:])
		effectiveNumUnits := uint32(stmt.ColumnInt32(5))
		a.SetEffectiveNumUnits(effectiveNumUnits)
		if checkpointed {
			a.SetGolden()
			a.NumUnits = effectiveNumUnits
			a.SetReceived(time.Time{})
		} else {
			a.SetReceived(time.Unix(0, stmt.ColumnInt64(6)).Local())
		}
		a.PublishEpoch = types.EpochID(uint32(stmt.ColumnInt(7)))
		a.Sequence = uint64(stmt.ColumnInt64(8))
		stmt.ColumnBytes(9, a.Coinbase[:])
		v, err := a.Verify(baseTickHeight, tickCount)
		if err != nil {
			return fn(nil, err)
		}
		return fn(v, nil)
	}
}

func load(db sql.Executor, query string, enc sql.Encoder) (*types.VerifiedActivationTx, error) {
	var (
		v    *types.VerifiedActivationTx
		derr error
	)
	_, err := db.Exec(query, enc, decoder(func(atx *types.VerifiedActivationTx, err error) bool {
		v = atx
		derr = err
		return derr == nil
	}))
	if err == nil && derr != nil {
		err = derr
	}
	return v, err
}

// Get gets an ATX by a given ATX ID.
func Get(db sql.Executor, id types.ATXID) (*types.VerifiedActivationTx, error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}
	q := fmt.Sprintf("%v where id =?1;", fullQuery)
	v, err := load(db, q, enc)
	if err != nil {
		return nil, fmt.Errorf("get id %s: %w", id.String(), err)
	}
	if v == nil {
		return nil, fmt.Errorf("get id %s: %w", id.String(), sql.ErrNotFound)
	}
	return v, nil
}

// GetByEpochAndNodeID gets any ATX by the specified NodeID published in the given epoch.
func GetByEpochAndNodeID(
	db sql.Executor,
	epoch types.EpochID,
	nodeID types.NodeID,
) (*types.VerifiedActivationTx, error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
		stmt.BindBytes(2, nodeID.Bytes())
	}
	q := fmt.Sprintf("%v where epoch = ?1 and pubkey = ?2 limit 1;", fullQuery)
	v, err := load(db, q, enc)
	if err != nil {
		return nil, fmt.Errorf("get by epoch %v nid %s: %w", epoch, nodeID.String(), err)
	}
	if v == nil {
		return nil, fmt.Errorf("get by epoch %v nid %s: %w", epoch, nodeID.String(), sql.ErrNotFound)
	}
	return v, nil
}

// Has checks if an ATX exists by a given ATX ID.
func Has(db sql.Executor, id types.ATXID) (bool, error) {
	rows, err := db.Exec("select 1 from atxs where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, nil,
	)
	if err != nil {
		return false, fmt.Errorf("exec id %v: %w", id, err)
	}
	return rows > 0, nil
}

func CommitmentATX(db sql.Executor, nodeID types.NodeID) (id types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, id[:])
		return true
	}

	if rows, err := db.Exec(`
		select commitment_atx from atxs
		where pubkey = ?1 and commitment_atx is not null
		order by epoch desc
		limit 1;`, enc, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("exec nodeID %v: %w", nodeID, err)
	} else if rows == 0 {
		return types.ATXID{}, fmt.Errorf("exec nodeID %s: %w", nodeID, sql.ErrNotFound)
	}

	return id, err
}

// GetFirstIDByNodeID gets the initial ATX ID for a given node ID.
func GetFirstIDByNodeID(db sql.Executor, nodeID types.NodeID) (id types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, id[:])
		return true
	}

	if rows, err := db.Exec(`
		select id from atxs
		where pubkey = ?1
		order by epoch asc
		limit 1;`, enc, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("exec nodeID %v: %w", nodeID, err)
	} else if rows == 0 {
		return types.ATXID{}, fmt.Errorf("exec nodeID %s: %w", nodeID, sql.ErrNotFound)
	}

	return id, err
}

// GetLastIDByNodeID gets the last ATX ID for a given node ID.
func GetLastIDByNodeID(db sql.Executor, nodeID types.NodeID) (id types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, id[:])
		return true
	}

	if rows, err := db.Exec(`
		select id from atxs
		where pubkey = ?1
		order by epoch desc, received desc
		limit 1;`, enc, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("exec nodeID %s: %w", nodeID, err)
	} else if rows == 0 {
		return types.ATXID{}, fmt.Errorf("exec nodeID %s: %w", nodeID, sql.ErrNotFound)
	}

	return id, err
}

// GetIDByEpochAndNodeID gets an ATX ID for a given epoch and node ID.
func GetIDByEpochAndNodeID(db sql.Executor, epoch types.EpochID, nodeID types.NodeID) (id types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
		stmt.BindBytes(2, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, id[:])
		return true
	}

	if rows, err := db.Exec(`
		select id from atxs
		where epoch = ?1 and pubkey = ?2
		limit 1;`, enc, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("exec nodeID %v: %w", nodeID, err)
	} else if rows == 0 {
		return types.ATXID{}, fmt.Errorf("exec nodeID %s: %w", nodeID, sql.ErrNotFound)
	}

	return id, err
}

// GetIDsByEpoch gets ATX IDs for a given epoch.
func GetIDsByEpoch(db sql.Executor, epoch types.EpochID) (ids []types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
	}
	dec := func(stmt *sql.Statement) bool {
		var id types.ATXID
		stmt.ColumnBytes(0, id[:])
		ids = append(ids, id)
		return true
	}

	if rows, err := db.Exec("select id from atxs where epoch = ?1;", enc, dec); err != nil {
		return nil, fmt.Errorf("exec epoch %v: %w", epoch, err)
	} else if rows == 0 {
		return []types.ATXID{}, nil
	}

	return ids, nil
}

// VRFNonce gets the VRF nonce of a smesher for a given epoch.
func VRFNonce(db sql.Executor, id types.NodeID, epoch types.EpochID) (nonce types.VRFPostIndex, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindInt64(2, int64(epoch))
	}
	dec := func(stmt *sql.Statement) bool {
		nonce = types.VRFPostIndex(stmt.ColumnInt64(0))
		return true
	}

	if rows, err := db.Exec(`
		select nonce from atxs
		where pubkey = ?1 and epoch < ?2 and nonce is not null
		order by epoch desc
		limit 1;`, enc, dec); err != nil {
		return types.VRFPostIndex(0), fmt.Errorf("exec id %v, epoch %d: %w", id, epoch, err)
	} else if rows == 0 {
		return types.VRFPostIndex(0), fmt.Errorf("exec id %v, epoch %d: %w", id, epoch, sql.ErrNotFound)
	}

	return nonce, err
}

// GetBlob loads ATX as an encoded blob, ready to be sent over the wire.
func GetBlob(db sql.Executor, id []byte) (buf []byte, err error) {
	if rows, err := db.Exec("select atx from atxs where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id)
		}, func(stmt *sql.Statement) bool {
			if stmt.ColumnLen(0) > 0 {
				buf = make([]byte, stmt.ColumnLen(0))
				stmt.ColumnBytes(0, buf)
			}
			return true
		}); err != nil {
		return nil, fmt.Errorf("get %s: %w", types.BytesToHash(id), err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w: atx %s", sql.ErrNotFound, types.BytesToHash(id))
	}
	return buf, nil
}

// Add adds an ATX for a given ATX ID.
func Add(db sql.Executor, atx *types.VerifiedActivationTx) error {
	buf, err := codec.Encode(atx.ActivationTx)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, atx.ID().Bytes())
		stmt.BindInt64(2, int64(atx.PublishEpoch))
		stmt.BindInt64(3, int64(atx.EffectiveNumUnits()))
		if atx.CommitmentATX != nil {
			stmt.BindBytes(4, atx.CommitmentATX.Bytes())
		} else {
			stmt.BindNull(4)
		}
		if atx.VRFNonce != nil {
			stmt.BindInt64(5, int64(*atx.VRFNonce))
		} else {
			stmt.BindNull(5)
		}
		stmt.BindBytes(6, atx.SmesherID.Bytes())
		stmt.BindBytes(7, buf)
		stmt.BindInt64(8, atx.Received().UnixNano())
		stmt.BindInt64(9, int64(atx.BaseTickHeight()))
		stmt.BindInt64(10, int64(atx.TickCount()))
		stmt.BindInt64(11, int64(atx.Sequence))
		stmt.BindBytes(12, atx.Coinbase.Bytes())
	}

	_, err = db.Exec(`
		insert into atxs (id, epoch, effective_num_units, commitment_atx, nonce,
			 pubkey, atx, received, base_tick_height, tick_count, sequence, coinbase)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12);`, enc, nil)
	if err != nil {
		return fmt.Errorf("insert ATX ID %v: %w", atx.ID(), err)
	}
	return nil
}

// GetIDWithMaxHeight returns the ID of the atx from the last 2 epoch with the highest (or tied for the highest)
// tick height. It is possible that some poet servers are faster than others and the network ends up having its
// highest ticked atx still in previous epoch and the atxs building on top of it have not been published yet.
// Selecting from the last two epochs to strike a balance between being fair to honest miners while not giving
// unfair advantage for malicious actors who retroactively publish a high tick atx many epochs back.
func GetIDWithMaxHeight(db sql.Executor, pref types.NodeID) (types.ATXID, error) {
	var (
		rst types.ATXID
		max uint64
	)
	dec := func(stmt *sql.Statement) bool {
		var id types.ATXID
		stmt.ColumnBytes(0, id[:])
		height := uint64(stmt.ColumnInt64(1)) + uint64(stmt.ColumnInt64(2))
		if height >= max {
			var smesher types.NodeID
			stmt.ColumnBytes(3, smesher[:])
			if height > max {
				max = height
				rst = id
			} else if pref != types.EmptyNodeID && smesher == pref {
				// height is equal. prefer atxs from `pref`
				rst = id
			}
		}
		return true
	}

	if rows, err := db.Exec(`
		select id, base_tick_height, tick_count, pubkey
		from atxs left join identities using(pubkey)
		where identities.pubkey is null and epoch >= (select max(epoch) from atxs)-1
		order by epoch desc;`, nil, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("select positioning atx: %w", err)
	} else if rows == 0 {
		return types.ATXID{}, sql.ErrNotFound
	}
	return rst, nil
}

type CheckpointAtx struct {
	ID             types.ATXID
	Epoch          types.EpochID
	CommitmentATX  types.ATXID
	VRFNonce       types.VRFPostIndex
	NumUnits       uint32
	BaseTickHeight uint64
	TickCount      uint64
	SmesherID      types.NodeID
	Sequence       uint64
	Coinbase       types.Address
}

// LatestN returns the latest N ATXs per smesher.
func LatestN(db sql.Executor, n int) ([]CheckpointAtx, error) {
	var rst []CheckpointAtx
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(n))
	}
	dec := func(stmt *sql.Statement) bool {
		var catx CheckpointAtx
		stmt.ColumnBytes(0, catx.ID[:])
		catx.Epoch = types.EpochID(uint32(stmt.ColumnInt64(1)))
		catx.NumUnits = uint32(stmt.ColumnInt64(2))
		catx.BaseTickHeight = uint64(stmt.ColumnInt64(3))
		catx.TickCount = uint64(stmt.ColumnInt64(4))
		stmt.ColumnBytes(5, catx.SmesherID[:])
		catx.Sequence = uint64(stmt.ColumnInt64(6))
		stmt.ColumnBytes(7, catx.Coinbase[:])
		rst = append(rst, catx)
		return true
	}

	if rows, err := db.Exec(`
		select id, epoch, effective_num_units, base_tick_height, tick_count, pubkey, sequence, coinbase
		from (
			select row_number() over (partition by pubkey order by epoch desc) RowNum,
			id, epoch, effective_num_units, base_tick_height, tick_count, pubkey, sequence, coinbase from atxs
		)
		where RowNum <= ?1 order by pubkey;`, enc, dec); err != nil {
		return nil, fmt.Errorf("latestN: %w", err)
	} else if rows == 0 {
		return nil, sql.ErrNotFound
	}
	return rst, nil
}

func AddCheckpointed(db sql.Executor, catx *CheckpointAtx) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, catx.ID.Bytes())
		stmt.BindInt64(2, int64(catx.Epoch))
		stmt.BindInt64(3, int64(catx.NumUnits))
		stmt.BindBytes(4, catx.CommitmentATX.Bytes())
		stmt.BindInt64(5, int64(catx.VRFNonce))
		stmt.BindInt64(6, int64(catx.BaseTickHeight))
		stmt.BindInt64(7, int64(catx.TickCount))
		stmt.BindInt64(8, int64(catx.Sequence))
		stmt.BindBytes(9, catx.SmesherID.Bytes())
		stmt.BindBytes(10, catx.Coinbase.Bytes())
	}

	_, err := db.Exec(`
		insert into atxs (id, epoch, effective_num_units, commitment_atx, nonce,
			base_tick_height, tick_count, sequence, pubkey, coinbase, received)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, 0);`, enc, nil)
	if err != nil {
		return fmt.Errorf("insert checkpoint ATX %v: %w", catx.ID, err)
	}
	return nil
}

// All gets all atx IDs.
func All(db sql.Executor) ([]types.ATXID, error) {
	var all []types.ATXID
	dec := func(stmt *sql.Statement) bool {
		var id types.ATXID
		stmt.ColumnBytes(0, id[:])
		all = append(all, id)
		return true
	}
	if _, err := db.Exec("select id from atxs order by epoch asc;", nil, dec); err != nil {
		return nil, fmt.Errorf("all atxs: %w", err)
	}
	return all, nil
}

// LatestEpoch with atxs.
func LatestEpoch(db sql.Executor) (types.EpochID, error) {
	var epoch types.EpochID
	if _, err := db.Exec("select max(epoch) from atxs;",
		nil,
		func(stmt *sql.Statement) bool {
			epoch = types.EpochID(uint32(stmt.ColumnInt64(0)))
			return true
		}); err != nil {
		return epoch, fmt.Errorf("latest epoch: %w", err)
	}
	return epoch, nil
}

func IterateAtxs(db sql.Executor, from, to types.EpochID, fn func(*types.VerifiedActivationTx) bool) error {
	var derr error
	_, err := db.Exec(fullQuery+" where epoch between ?1 and ?2", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(from.Uint32()))
		stmt.BindInt64(2, int64(to.Uint32()))
	}, decoder(func(atx *types.VerifiedActivationTx, err error) bool {
		if atx != nil {
			return fn(atx)
		}
		derr = err
		return derr == nil
	}))
	if err != nil {
		return err
	}
	return derr
}
