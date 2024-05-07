package atxs

import (
	"context"
	"errors"
	"fmt"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
)

const (
	CacheKindEpochATXs sql.QueryCacheKind = "epoch-atxs"
	CacheKindATXBlob   sql.QueryCacheKind = "atx-blob"
)

// Query to retrieve ATXs.
// Can't use inner join for the ATX blob here b/c this will break
// filters that refer to the id column.
const fullQuery = `select id,
        (select atx from atx_blobs b where a.id = b.id) as atx,
        base_tick_height, tick_count, pubkey,
	effective_num_units, received, epoch, sequence, coinbase, validity
	from atxs a`

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
			var atxV1 wire.ActivationTxV1
			if _, err := codec.DecodeFrom(stmt.ColumnReader(1), &atxV1); err != nil {
				return fn(nil, fmt.Errorf("decode %w", err))
			}
			a = *wire.ActivationTxFromWireV1(&atxV1)
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
		a.SetValidity(types.Validity(stmt.ColumnInt(10)))
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

// PrevIDByNodeID returns the previous ATX ID for a given node ID and public epoch.
// It returns the newest ATX ID that was published before the given public epoch.
func PrevIDByNodeID(db sql.Executor, nodeID types.NodeID, pubEpoch types.EpochID) (id types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(pubEpoch))
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, id[:])
		return true
	}

	if rows, err := db.Exec(`
		select id from atxs
		where pubkey = ?1 and epoch < ?2
		order by epoch desc
		limit 1;`, enc, dec); err != nil {
		return types.EmptyATXID, fmt.Errorf("exec nodeID %v, epoch %d: %w", nodeID, pubEpoch, err)
	} else if rows == 0 {
		return types.EmptyATXID, fmt.Errorf("exec nodeID %s, epoch %d: %w", nodeID, pubEpoch, sql.ErrNotFound)
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
func GetIDsByEpoch(ctx context.Context, db sql.Executor, epoch types.EpochID) (ids []types.ATXID, err error) {
	cacheKey := sql.QueryCacheKey(CacheKindEpochATXs, epoch.String())
	return sql.WithCachedValue(ctx, db, cacheKey, func(context.Context) (ids []types.ATXID, err error) {
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
	})
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

// GetBlobSizes returns the sizes of the blobs corresponding to ATXs with specified
// ids. For non-existent ATXs, the corresponding items are set to -1.
func GetBlobSizes(db sql.Executor, ids [][]byte) (sizes []int, err error) {
	return sql.GetBlobSizes(db, "select id, length(atx) from atx_blobs where id in", ids)
}

// LoadBlob loads ATX as an encoded blob, ready to be sent over the wire.
func LoadBlob(ctx context.Context, db sql.Executor, id []byte, blob *sql.Blob) error {
	if sql.IsCached(db) {
		b, err := getBlob(ctx, db, id)
		if err != nil {
			return err
		}
		blob.Bytes = b
		return nil
	}
	return sql.LoadBlob(db, "select atx from atx_blobs where id = ?1", id, blob)
}

func getBlob(ctx context.Context, db sql.Executor, id []byte) (buf []byte, err error) {
	cacheKey := sql.QueryCacheKey(CacheKindATXBlob, string(id))
	return sql.WithCachedValue(ctx, db, cacheKey, func(context.Context) ([]byte, error) {
		if rows, err := db.Exec("select atx from atx_blobs where id = ?1",
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
	})
}

// NonceByID retrieves VRFNonce corresponding to the specified ATX ID.
func NonceByID(db sql.Executor, id types.ATXID) (nonce types.VRFPostIndex, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}
	gotNonce := false
	dec := func(stmt *sql.Statement) bool {
		if stmt.ColumnType(0) != sqlite.SQLITE_NULL {
			nonce = types.VRFPostIndex(stmt.ColumnInt64(0))
			gotNonce = true
		}
		return true
	}

	if rows, err := db.Exec("select nonce from atxs where id = ?1", enc, dec); err != nil {
		return types.VRFPostIndex(0), fmt.Errorf("get nonce for ATX id %v: %w", id, err)
	} else if rows == 0 || !gotNonce {
		return types.VRFPostIndex(0), sql.ErrNotFound
	}

	return nonce, err
}

// Add adds an ATX for a given ATX ID.
func Add(db sql.Executor, atx *types.VerifiedActivationTx) error {
	_, err := AddGettingNonce(db, atx)
	return err
}

// AddGettingNonce adds an ATX for a given ATX ID and returns the nonce for the newly added ATX.
func AddGettingNonce(db sql.Executor, atx *types.VerifiedActivationTx) (*types.VRFPostIndex, error) {
	if atx.ActivationTx.VRFNonce == nil && atx.PrevATXID != types.EmptyATXID {
		nonce, err := NonceByID(db, atx.PrevATXID)
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return nil, fmt.Errorf("error getting nonce: %w", err)
		}
		if err == nil {
			err = add(db, atx, &nonce)
			if err != nil {
				return &nonce, err
			} else {
				return &nonce, nil
			}
		}
	}

	if err := add(db, atx, atx.VRFNonce); err != nil {
		return nil, err
	}

	return atx.VRFNonce, nil
}

// AddMaybeNoNonce adds an ATX for a given ATX ID. It doesn't try
// to set the nonce field if VRFNonce is not set in the ATX.
// This function is only to be used for testing.
func AddMaybeNoNonce(db sql.Executor, atx *types.VerifiedActivationTx) error {
	return add(db, atx, atx.VRFNonce)
}

func add(db sql.Executor, atx *types.VerifiedActivationTx, nonce *types.VRFPostIndex) error {
	buf, err := codec.Encode(wire.ActivationTxToWireV1(atx.ActivationTx))
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
		if nonce != nil {
			stmt.BindInt64(5, int64(*nonce))
		} else {
			stmt.BindNull(5)
		}
		stmt.BindBytes(6, atx.SmesherID.Bytes())
		stmt.BindInt64(7, atx.Received().UnixNano())
		stmt.BindInt64(8, int64(atx.BaseTickHeight()))
		stmt.BindInt64(9, int64(atx.TickCount()))
		stmt.BindInt64(10, int64(atx.Sequence))
		stmt.BindBytes(11, atx.Coinbase.Bytes())
		stmt.BindInt64(12, int64(atx.Validity()))
		if atx.PrevATXID != types.EmptyATXID {
			stmt.BindBytes(13, atx.PrevATXID.Bytes())
		} else {
			stmt.BindNull(13)
		}
	}

	_, err = db.Exec(`
		insert into atxs (id, epoch, effective_num_units, commitment_atx, nonce,
			 pubkey, received, base_tick_height, tick_count, sequence, coinbase,
			 validity, prev_id)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13)`, enc, nil)
	if err != nil {
		return fmt.Errorf("insert ATX ID %v: %w", atx.ID(), err)
	}

	enc = func(stmt *sql.Statement) {
		stmt.BindBytes(1, atx.ID().Bytes())
		stmt.BindBytes(2, buf)
	}
	_, err = db.Exec("insert into atx_blobs (id, atx) values (?1, ?2)", enc, nil)
	if err != nil {
		return fmt.Errorf("insert ATX blob %v: %w", atx.ID(), err)
	}

	return nil
}

// AtxAdded updates epoch query cache with new ATX, if the query cache is enabled.
func AtxAdded(db sql.Executor, atx *types.VerifiedActivationTx) {
	epochCacheKey := sql.QueryCacheKey(CacheKindEpochATXs, atx.PublishEpoch.String())
	sql.AppendToCachedSlice(db, epochCacheKey, atx.ID())
}

type Filter func(types.ATXID) bool

func FilterAll(types.ATXID) bool { return true }

// GetIDWithMaxHeight returns the ID of the atx from the last 2 epoch with the highest (or tied for the highest)
// tick height. It is possible that some poet servers are faster than others and the network ends up having its
// highest ticked atx still in previous epoch and the atxs building on top of it have not been published yet.
// Selecting from the last two epochs to strike a balance between being fair to honest miners while not giving
// unfair advantage for malicious actors who retroactively publish a high tick atx many epochs back.
func GetIDWithMaxHeight(db sql.Executor, pref types.NodeID, filter Filter) (types.ATXID, error) {
	if filter == nil {
		filter = FilterAll
	}
	var (
		rst     types.ATXID
		highest uint64
	)
	dec := func(stmt *sql.Statement) bool {
		var id types.ATXID
		stmt.ColumnBytes(0, id[:])
		height := uint64(stmt.ColumnInt64(1))

		switch {
		case height < highest:
			// Results are ordered by height, so we can stop once we see a lower height.
			return false
		case height > highest && filter(id):
			highest = height
			rst = id
			// We can stop on the first ATX if `pref` is empty.
			return pref != types.EmptyNodeID
		case height == highest && filter(id):
			// prefer atxs from `pref`
			var smesher types.NodeID
			stmt.ColumnBytes(2, smesher[:])
			if smesher == pref {
				rst = id
				return false
			}
			return true
		}

		return true
	}

	_, err := db.Exec(`
	SELECT id, base_tick_height + tick_count AS height, pubkey
	FROM atxs LEFT JOIN identities using(pubkey)
	WHERE identities.pubkey is null and epoch >= (select max(epoch) from atxs)-1
	ORDER BY height DESC, epoch DESC;`, nil, dec)
	switch {
	case err != nil:
		return types.ATXID{}, fmt.Errorf("selecting high-tick atx: %w", err)
	case rst == types.EmptyATXID:
		return types.ATXID{}, fmt.Errorf("selecting high-tick atx: %w", sql.ErrNotFound)
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
	var (
		rst  []CheckpointAtx
		ierr error
	)
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
		if sql.IsNull(stmt, 8) {
			ierr = errors.New("missing nonce")
			return false
		} else {
			catx.VRFNonce = types.VRFPostIndex(stmt.ColumnInt64(8))
		}
		rst = append(rst, catx)
		return true
	}

	if rows, err := db.Exec(`
		select id, epoch, effective_num_units, base_tick_height, tick_count, pubkey, sequence, coinbase, nonce
		from (
			select row_number() over (partition by pubkey order by epoch desc) RowNum,
			id, epoch, effective_num_units, base_tick_height, tick_count, pubkey, sequence, coinbase, nonce
			from atxs
		)
		where RowNum <= ?1 order by pubkey;`, enc, dec); err != nil {
		return nil, fmt.Errorf("latestN: %w", err)
	} else if rows == 0 {
		return nil, sql.ErrNotFound
	} else if ierr != nil {
		return nil, ierr
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
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, 0)`, enc, nil)
	if err != nil {
		return fmt.Errorf("insert checkpoint ATX %v: %w", catx.ID, err)
	}
	enc = func(stmt *sql.Statement) {
		stmt.BindBytes(1, catx.ID.Bytes())
	}
	_, err = db.Exec("insert into atx_blobs (id) values (?1)", enc, nil)
	if err != nil {
		return fmt.Errorf("insert checkpoint ATX blob %v: %w", catx.ID, err)
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

// IterateAtxsData iterate over data used for consensus.
func IterateAtxsData(
	db sql.Executor,
	from, to types.EpochID,
	fn func(
		id types.ATXID,
		node types.NodeID,
		epoch types.EpochID,
		coinbase types.Address,
		weight uint64,
		base uint64,
		height uint64,
		nonce *types.VRFPostIndex,
		isMalicious bool,
	) bool,
) error {
	_, err := db.Exec(
		`select
                   a.id, a.pubkey, a.epoch, a.coinbase, a.effective_num_units,
                   a.base_tick_height, a.tick_count, a.nonce,
                   iif(idn.proof is null, 0, 1) as is_malicious
		from atxs a left join identities idn on a.pubkey = idn.pubkey`,
		// SQLite happens to process the query much faster if we don't
		// filter it by epoch
		// where a.epoch between ? and ?`,
		// func(stmt *sql.Statement) {
		// 	stmt.BindInt64(1, int64(from.Uint32()))
		// 	stmt.BindInt64(2, int64(to.Uint32()))
		// },
		nil,
		func(stmt *sql.Statement) bool {
			epoch := types.EpochID(uint32(stmt.ColumnInt64(2)))
			if epoch < from || epoch > to {
				return true
			}
			var id types.ATXID
			stmt.ColumnBytes(0, id[:])
			var node types.NodeID
			stmt.ColumnBytes(1, node[:])
			var coinbase types.Address
			stmt.ColumnBytes(3, coinbase[:])
			effectiveUnits := uint64(stmt.ColumnInt64(4))
			baseHeight := uint64(stmt.ColumnInt64(5))
			ticks := uint64(stmt.ColumnInt64(6))
			var vrfNonce *types.VRFPostIndex
			if !sql.IsNull(stmt, 7) {
				nonce := types.VRFPostIndex(stmt.ColumnInt64(7))
				vrfNonce = &nonce
			}
			isMalicious := stmt.ColumnInt(8) != 0
			return fn(id, node, epoch, coinbase, effectiveUnits*ticks,
				baseHeight, baseHeight+ticks, vrfNonce, isMalicious)
		},
	)
	if err != nil {
		return fmt.Errorf("iterate atx fields: %w", err)
	}
	return nil
}

func SetValidity(db sql.Executor, id types.ATXID, validity types.Validity) error {
	_, err := db.Exec("UPDATE atxs SET validity = ?1 where id = ?2;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(validity))
			stmt.BindBytes(2, id.Bytes())
		}, nil,
	)
	if err != nil {
		return fmt.Errorf("setting validity %v: %w", id, err)
	}
	return nil
}

func IterateAtxsOps(
	db sql.Executor,
	operations builder.Operations,
	fn func(*types.VerifiedActivationTx) bool,
) error {
	var derr error
	_, err := db.Exec(
		fullQuery+builder.FilterFrom(operations),
		builder.BindingsFrom(operations),
		decoder(func(atx *types.VerifiedActivationTx, err error) bool {
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

func CountAtxsByOps(db sql.Executor, operations builder.Operations) (count uint32, err error) {
	_, err = db.Exec(
		"SELECT count(*) FROM atxs"+builder.FilterFrom(operations),
		builder.BindingsFrom(operations),
		func(stmt *sql.Statement) bool {
			count = uint32(stmt.ColumnInt32(0))
			return true
		},
	)
	return
}

// IterateForGrading selects every atx from publish epoch and joins identities to load malfeasence proofs if they exist.
func IterateForGrading(
	db sql.Executor,
	epoch types.EpochID,
	fn func(id types.ATXID, atxtime, prooftime int64, weight uint64) bool,
) error {
	if _, err := db.Exec(`
		select atxs.id, atxs.received, identities.received, effective_num_units, tick_count from atxs
		left join identities on atxs.pubkey = identities.pubkey
		where atxs.epoch == ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(epoch))
		}, func(stmt *sql.Statement) bool {
			id := types.ATXID{}
			stmt.ColumnBytes(0, id[:])
			atxtime := stmt.ColumnInt64(1)
			prooftime := stmt.ColumnInt64(2)
			units := uint64(stmt.ColumnInt64(3))
			ticks := uint64(stmt.ColumnInt64(4))
			return fn(id, atxtime, prooftime, units*ticks)
		}); err != nil {
		return fmt.Errorf("iterate for grading: %w", err)
	}
	return nil
}

func PoetProofRef(ctx context.Context, db sql.Executor, id types.ATXID) (types.PoetProofRef, error) {
	var blob sql.Blob
	if err := LoadBlob(ctx, db, id.Bytes(), &blob); err != nil {
		return types.PoetProofRef{}, fmt.Errorf("getting blob for %s: %w", id, err)
	}

	// TODO: decide about version based on publish epoch
	var atx wire.ActivationTxV1
	if err := codec.Decode(blob.Bytes, &atx); err != nil {
		return types.PoetProofRef{}, fmt.Errorf("decoding ATX blob: %w", err)
	}

	return types.PoetProofRef(atx.NIPost.PostMetadata.Challenge), nil
}

type PrevATXCollision struct {
	NodeID1 types.NodeID
	ATX1    types.ATXID

	NodeID2 types.NodeID
	ATX2    types.ATXID
}

func PrevATXCollisions(db sql.Executor) ([]PrevATXCollision, error) {
	var result []PrevATXCollision

	dec := func(stmt *sql.Statement) bool {
		var nodeID1, nodeID2 types.NodeID
		stmt.ColumnBytes(0, nodeID1[:])
		stmt.ColumnBytes(1, nodeID2[:])

		var id1, id2 types.ATXID
		stmt.ColumnBytes(2, id1[:])
		stmt.ColumnBytes(3, id2[:])

		result = append(result, PrevATXCollision{
			NodeID1: nodeID1,
			ATX1:    id1,

			NodeID2: nodeID2,
			ATX2:    id2,
		})
		return true
	}
	if _, err := db.Exec(`
		SELECT t1.pubkey, t2.pubkey, t1.id, t2.id
		FROM atxs t1
		INNER JOIN atxs t2 ON t1.prev_id = t2.prev_id
		WHERE t1.id < t2.id;`, nil, dec); err != nil {
		return nil, fmt.Errorf("error getting ATXs with same prevATX: %w", err)
	}

	return result, nil
}
