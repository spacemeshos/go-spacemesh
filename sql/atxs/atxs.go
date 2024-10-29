package atxs

import (
	"context"
	"fmt"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"

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
const fieldsQuery = `select
atxs.id, atxs.nonce, atxs.base_tick_height, atxs.tick_count, atxs.pubkey, atxs.effective_num_units,
atxs.received, atxs.epoch, atxs.sequence, atxs.coinbase, atxs.validity, atxs.commitment_atx, atxs.weight,
atxs.marriage_atx`

const fullQuery = fieldsQuery + ` from atxs`

type decoderCallback func(*types.ActivationTx) bool

func decoder(fn decoderCallback) sql.Decoder {
	return func(stmt *sql.Statement) bool {
		var (
			a  types.ActivationTx
			id types.ATXID
		)
		stmt.ColumnBytes(0, id[:])
		a.SetID(id)
		a.VRFNonce = types.VRFPostIndex(stmt.ColumnInt64(1))
		a.BaseTickHeight = uint64(stmt.ColumnInt64(2))
		a.TickCount = uint64(stmt.ColumnInt64(3))
		stmt.ColumnBytes(4, a.SmesherID[:])
		a.NumUnits = uint32(stmt.ColumnInt32(5))
		// Note: received is assigned `0` for checkpointed ATXs.
		// We treat `0` as 'zero time'.
		// We could use `NULL` instead, but the column has "NOT NULL" constraint.
		// In future, consider changing the schema to allow `NULL` for received.
		if received := stmt.ColumnInt64(6); received == 0 {
			a.SetGolden()
		} else {
			a.SetReceived(time.Unix(0, received).Local())
		}
		a.PublishEpoch = types.EpochID(uint32(stmt.ColumnInt(7)))
		a.Sequence = uint64(stmt.ColumnInt64(8))
		stmt.ColumnBytes(9, a.Coinbase[:])
		a.SetValidity(types.Validity(stmt.ColumnInt(10)))
		if stmt.ColumnType(11) != sqlite.SQLITE_NULL {
			a.CommitmentATX = new(types.ATXID)
			stmt.ColumnBytes(11, a.CommitmentATX[:])
		}
		a.Weight = uint64(stmt.ColumnInt64(12))
		if stmt.ColumnType(13) != sqlite.SQLITE_NULL {
			a.MarriageATX = new(types.ATXID)
			stmt.ColumnBytes(13, a.MarriageATX[:])
		}

		return fn(&a)
	}
}

func load(db sql.Executor, query string, enc sql.Encoder) (*types.ActivationTx, error) {
	var v *types.ActivationTx
	_, err := db.Exec(query, enc, decoder(func(atx *types.ActivationTx) bool {
		v = atx
		return true
	}))
	return v, err
}

// Get gets an ATX by a given ATX ID.
func Get(db sql.Executor, id types.ATXID) (*types.ActivationTx, error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}
	q := fmt.Sprintf("%s where id =?1;", fullQuery)
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
) (types.ATXID, error) {
	var id types.ATXID
	rows, err := db.Exec("select id from atxs where epoch = ?1 and pubkey = ?2 limit 1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(epoch))
			stmt.BindBytes(2, nodeID.Bytes())
		},
		func(stmt *sql.Statement) bool {
			stmt.ColumnBytes(0, id[:])
			return false
		},
	)
	if err != nil {
		return types.EmptyATXID, fmt.Errorf("get by epoch %v nid %s: %w", epoch, nodeID.String(), err)
	}
	if rows == 0 {
		return types.EmptyATXID, fmt.Errorf("get by epoch %v nid %s: %w", epoch, nodeID.String(), sql.ErrNotFound)
	}
	return id, nil
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

// IdentityExists checks if an identity has ever published an ATX.
func IdentityExists(db sql.Executor, nodeID types.NodeID) (bool, error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}

	rows, err := db.Exec(`
		select 1 from atxs
		where pubkey = ?1
		limit 1;`, enc, nil)
	if err != nil {
		return false, fmt.Errorf("exec nodeID %v: %w", nodeID, err)
	} else if rows == 0 {
		return false, nil
	}

	return true, nil
}

// Coinbase retrieves the last coinbase address used by the given node ID.
func Coinbase(db sql.Executor, id types.NodeID) (types.Address, error) {
	var coinbase types.Address
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, coinbase[:])
		return true
	}

	if rows, err := db.Exec(`
		select coinbase from atxs
		where pubkey = ?1
		order by epoch desc
		limit 1;`, enc, dec); err != nil {
		return types.Address{}, fmt.Errorf("looking up coinbase for smesherID %v: %w", id, err)
	} else if rows == 0 {
		return types.Address{}, fmt.Errorf("looking up coinbase for smesherID %v: %w", id, sql.ErrNotFound)
	}

	return coinbase, nil
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
// It returns the newest ATX ID containing PoST of the given node ID
// that was published before the given public epoch.
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
		SELECT atxid FROM posts
		WHERE pubkey = ?1 AND publish_epoch < ?2
		ORDER BY publish_epoch DESC
		LIMIT 1;`, enc, dec); err != nil {
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
func LoadBlob(ctx context.Context, db sql.Executor, id []byte, blob *sql.Blob) (types.AtxVersion, error) {
	if sql.IsCached(db) {
		type cachedBlob struct {
			version types.AtxVersion
			buf     []byte
		}
		cacheKey := sql.QueryCacheKey(CacheKindATXBlob, string(id))
		cached, err := sql.WithCachedValue(ctx, db, cacheKey, func(context.Context) (*cachedBlob, error) {
			// We don't use the provided blob in this case to avoid
			// caching references to the underlying slice (subsequent calls would modify it).
			var blob sql.Blob
			v, err := getBlob(ctx, db, id, &blob)
			if err != nil {
				return nil, err
			}
			return &cachedBlob{version: v, buf: blob.Bytes}, nil
		})
		if err != nil {
			return 0, err
		}
		blob.Resize(len(cached.buf))
		copy(blob.Bytes, cached.buf)
		return cached.version, nil
	}

	return getBlob(ctx, db, id, blob)
}

func getBlob(ctx context.Context, db sql.Executor, id []byte, blob *sql.Blob) (types.AtxVersion, error) {
	var version types.AtxVersion
	rows, err := db.Exec("select atx, version from atx_blobs where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id)
		}, func(stmt *sql.Statement) bool {
			blob.FromColumn(stmt, 0)
			version = types.AtxVersion(stmt.ColumnInt(1))
			return true
		},
	)
	if err != nil {
		return 0, fmt.Errorf("get %v: %w", types.BytesToHash(id), err)
	}
	if rows == 0 {
		return 0, fmt.Errorf("%w: atx %s", sql.ErrNotFound, types.BytesToATXID(id))
	}

	// The migration adding the version column does not set it to 1 for existing ATXs.
	// Thus, both values 0 and 1 mean V1.
	if version == 0 {
		version = types.AtxV1
	}
	return version, nil
}

// Previous gets all previous ATXs for a given ATX ID.
func Previous(db sql.Executor, id types.ATXID) ([]types.ATXID, error) {
	var previous []types.ATXID
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		var prev types.ATXID
		if stmt.ColumnType(0) != sqlite.SQLITE_NULL {
			stmt.ColumnBytes(0, prev[:])
		}
		// Index is returned in descending order, so the first one defines the length of the slice.
		index := stmt.ColumnInt(1)
		if previous == nil {
			previous = make([]types.ATXID, index+1)
		}
		previous[index] = prev
		return true
	}

	rows, err := db.Exec(
		"SELECT prev_atxid, prev_atx_index FROM posts WHERE atxid = ?1 ORDER BY prev_atx_index DESC;",
		enc,
		dec,
	)
	switch {
	case err != nil:
		return nil, fmt.Errorf("previous ATXs for ATX ID %v: %w", id, err)
	case rows == 0:
		return nil, sql.ErrNotFound
	}
	return previous, nil
}

// NonceByID retrieves VRFNonce corresponding to the specified ATX ID.
func NonceByID(db sql.Executor, id types.ATXID) (nonce types.VRFPostIndex, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		nonce = types.VRFPostIndex(stmt.ColumnInt64(0))
		return false
	}

	if rows, err := db.Exec("select nonce from atxs where id = ?1", enc, dec); err != nil {
		return types.VRFPostIndex(0), fmt.Errorf("get nonce for ATX id %v: %w", id, err)
	} else if rows == 0 {
		return types.VRFPostIndex(0), sql.ErrNotFound
	}

	return nonce, err
}

func Add(db sql.Executor, atx *types.ActivationTx, blob types.AtxBlob) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, atx.ID().Bytes())
		stmt.BindInt64(2, int64(atx.PublishEpoch))
		stmt.BindInt64(3, int64(atx.NumUnits))
		if atx.CommitmentATX != nil {
			stmt.BindBytes(4, atx.CommitmentATX.Bytes())
		}
		stmt.BindInt64(5, int64(atx.VRFNonce))
		stmt.BindBytes(6, atx.SmesherID.Bytes())
		stmt.BindInt64(7, atx.Received().UnixNano())
		stmt.BindInt64(8, int64(atx.BaseTickHeight))
		stmt.BindInt64(9, int64(atx.TickCount))
		stmt.BindInt64(10, int64(atx.Sequence))
		stmt.BindBytes(11, atx.Coinbase.Bytes())
		stmt.BindInt64(12, int64(atx.Validity()))
		stmt.BindInt64(13, int64(atx.Weight))
		if atx.MarriageATX != nil {
			stmt.BindBytes(14, atx.MarriageATX.Bytes())
		}
	}

	_, err := db.Exec(`
		insert into atxs (id, epoch, effective_num_units, commitment_atx, nonce,
			 pubkey, received, base_tick_height, tick_count, sequence, coinbase,
			 validity, weight, marriage_atx)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14)`, enc, nil)
	if err != nil {
		return fmt.Errorf("insert ATX ID %v: %w", atx.ID(), err)
	}

	return AddBlob(db, atx.ID(), blob.Blob, blob.Version)
}

func AddBlob(db sql.Executor, id types.ATXID, blob []byte, version types.AtxVersion) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindBytes(2, blob)
		stmt.BindInt64(3, int64(version))
	}

	_, err := db.Exec("insert into atx_blobs (id, atx, version) values (?1, ?2, ?3)", enc, nil)
	if err != nil {
		return fmt.Errorf("insert ATX blob %v: %w", id, err)
	}
	return nil
}

// AtxAdded updates epoch query cache with new ATX, if the query cache is enabled.
func AtxAdded(db sql.Executor, atx *types.ActivationTx) {
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
	MarriageATX    *types.ATXID
	VRFNonce       types.VRFPostIndex
	BaseTickHeight uint64
	TickCount      uint64
	SmesherID      types.NodeID
	Sequence       uint64
	Coinbase       types.Address
	// total effective units
	NumUnits uint32
	// actual units of each included smesher
	Units map[types.NodeID]uint32
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
		catx.VRFNonce = types.VRFPostIndex(stmt.ColumnInt64(8))
		if stmt.ColumnType(9) != sqlite.SQLITE_NULL {
			catx.MarriageATX = new(types.ATXID)
			stmt.ColumnBytes(9, catx.MarriageATX[:])
		}
		rst = append(rst, catx)
		return true
	}

	rows, err := db.Exec(`
		select
		id, epoch, effective_num_units, base_tick_height, tick_count, pubkey, sequence, coinbase, nonce, marriage_atx
		from (
			select row_number() over (partition by pubkey order by epoch desc) RowNum,
			id, epoch, effective_num_units, base_tick_height, tick_count, pubkey, sequence, coinbase, nonce,
			marriage_atx
			from atxs
		)
		where RowNum <= ?1 order by pubkey;`, enc, dec)
	switch {
	case err != nil:
		return nil, fmt.Errorf("latestN: %w", err)
	case rows == 0:
		return nil, sql.ErrNotFound
	case ierr != nil:
		return nil, ierr
	}

	for i := range rst {
		units, err := AllUnits(db, rst[i].ID)
		if err != nil {
			return nil, fmt.Errorf("fetching units for ATX %s: %w", rst[i].ID, err)
		}
		rst[i].Units = units
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
		if catx.MarriageATX != nil {
			stmt.BindBytes(11, catx.MarriageATX.Bytes())
		}
	}

	_, err := db.Exec(`
		insert into atxs (id, epoch, effective_num_units, commitment_atx, nonce,
			base_tick_height, tick_count, sequence, pubkey, coinbase, marriage_atx, received)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, 0)`, enc, nil)
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

	for id, units := range catx.Units {
		// FIXME: should a checkpointed ATX reference its real previous ATX?
		if err := SetPost(db, catx.ID, types.EmptyATXID, 0, id, units, catx.Epoch); err != nil {
			return fmt.Errorf("insert checkpoint ATX units %v: %w", catx.ID, err)
		}
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
		nonce types.VRFPostIndex,
	) bool,
) error {
	_, err := db.Exec(
		`SELECT id, pubkey, epoch, coinbase, effective_num_units, base_tick_height, tick_count, nonce FROM atxs
		WHERE epoch between ?1 and ?2`,
		// filtering in CODE is no longer effective on some machines in epoch 29
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(from.Uint32()))
			stmt.BindInt64(2, int64(to.Uint32()))
		},
		func(stmt *sql.Statement) bool {
			epoch := types.EpochID(uint32(stmt.ColumnInt64(2)))
			var id types.ATXID
			stmt.ColumnBytes(0, id[:])
			var node types.NodeID
			stmt.ColumnBytes(1, node[:])
			var coinbase types.Address
			stmt.ColumnBytes(3, coinbase[:])
			effectiveUnits := uint64(stmt.ColumnInt64(4))
			baseHeight := uint64(stmt.ColumnInt64(5))
			ticks := uint64(stmt.ColumnInt64(6))
			nonce := types.VRFPostIndex(stmt.ColumnInt64(7))
			return fn(id, node, epoch, coinbase, effectiveUnits*ticks, baseHeight, baseHeight+ticks, nonce)
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
	fn func(*types.ActivationTx) bool,
) error {
	_, err := db.Exec(
		fullQuery+builder.FilterFrom(operations),
		builder.BindingsFrom(operations),
		decoder(fn),
	)
	return err
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

// IterateForGrading selects every atx from publish epoch and joins identities to load malfeasance proofs if they exist.
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

func IterateAtxsWithMalfeasance(
	db sql.Executor,
	publish types.EpochID,
	fn func(atx *types.ActivationTx, malicious bool) bool,
) error {
	query := fieldsQuery + `, iif(i.proof is null, 0, 1) as malicious
	FROM atxs left join identities i on atxs.pubkey = i.pubkey WHERE atxs.epoch = $1`

	_, err := db.Exec(
		query,
		func(s *sql.Statement) { s.BindInt64(1, int64(publish)) },
		func(s *sql.Statement) bool {
			return decoder(func(atx *types.ActivationTx) bool {
				return fn(atx, s.ColumnInt(14) != 0)
			})(s)
		},
	)
	return err
}

func IterateAtxIdsWithMalfeasance(
	db sql.Executor,
	publish types.EpochID,
	fn func(id types.ATXID, malicious bool) bool,
) error {
	query := `select id, iif(i.proof is null, 0, 1) as malicious
	FROM atxs left join identities i on atxs.pubkey = i.pubkey WHERE atxs.epoch = $1`

	_, err := db.Exec(
		query,
		func(s *sql.Statement) { s.BindInt64(1, int64(publish)) },
		func(s *sql.Statement) bool {
			var id types.ATXID
			s.ColumnBytes(0, id[:])
			return fn(id, s.ColumnInt(1) != 0)
		},
	)
	return err
}

func PrevATXCollision(db sql.Executor, prev types.ATXID, id types.NodeID) (types.ATXID, types.ATXID, error) {
	var atxs []types.ATXID
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, prev[:])
		stmt.BindBytes(2, id[:])
	}
	dec := func(stmt *sql.Statement) bool {
		var id types.ATXID
		stmt.ColumnBytes(0, id[:])
		atxs = append(atxs, id)
		return len(atxs) < 2
	}
	_, err := db.Exec("SELECT atxid FROM posts WHERE prev_atxid = ?1 AND pubkey = ?2;", enc, dec)
	if err != nil {
		return types.EmptyATXID, types.EmptyATXID, fmt.Errorf("error getting ATXs with same prevATX: %w", err)
	}
	if len(atxs) != 2 {
		return types.EmptyATXID, types.EmptyATXID, sql.ErrNotFound
	}
	return atxs[0], atxs[1], nil
}

func Units(db sql.Executor, atxID types.ATXID, nodeID types.NodeID) (uint32, error) {
	var units uint32
	rows, err := db.Exec(`
		SELECT units FROM posts WHERE atxid = ?1 AND pubkey = ?2;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, atxID.Bytes())
			stmt.BindBytes(2, nodeID.Bytes())
		},
		func(stmt *sql.Statement) bool {
			units = uint32(stmt.ColumnInt64(0))
			return false
		},
	)
	if rows == 0 {
		return 0, sql.ErrNotFound
	}
	return units, err
}

// FindDoublePublish finds 2 distinct ATXIDs that the given identity contributed PoST to in the given epoch.
//
// It is guaranteed to return 2 distinct ATXs when the error is nil.
// It works by finding an ATX in the given epoch that has a PoST contribution from the given identity.
// - `epoch` is looked up in the `atxs` table by matching atxid.
func FindDoublePublish(db sql.Executor, nodeID types.NodeID, epoch types.EpochID) ([]types.ATXID, error) {
	var ids []types.ATXID
	rows, err := db.Exec(`
		SELECT p.atxid
		FROM posts p
		INNER JOIN atxs a ON p.atxid = a.id
		WHERE p.pubkey = ?1 AND a.epoch = ?2;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
			stmt.BindInt64(2, int64(epoch))
		},
		func(stmt *sql.Statement) bool {
			var id types.ATXID
			stmt.ColumnBytes(0, id[:])
			ids = append(ids, id)
			return len(ids) < 2
		},
	)
	if err != nil {
		return nil, err
	}
	if rows != 2 {
		return nil, sql.ErrNotFound
	}
	return ids, nil
}

func AllUnits(db sql.Executor, id types.ATXID) (map[types.NodeID]uint32, error) {
	units := make(map[types.NodeID]uint32)
	rows, err := db.Exec(
		`SELECT pubkey, units FROM posts WHERE atxid = ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		},
		func(stmt *sql.Statement) bool {
			var nid types.NodeID
			stmt.ColumnBytes(0, nid[:])
			units[nid] = uint32(stmt.ColumnInt64(1))
			return true
		},
	)
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, sql.ErrNotFound
	}
	return units, nil
}

func SetPost(
	db sql.Executor,
	atxID, prev types.ATXID,
	prevIndex int,
	id types.NodeID,
	units uint32,
	publish types.EpochID,
) error {
	_, err := db.Exec(
		`INSERT INTO posts (atxid, pubkey, prev_atxid, prev_atx_index, units, publish_epoch)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, atxID.Bytes())
			stmt.BindBytes(2, id.Bytes())
			if prev != types.EmptyATXID {
				stmt.BindBytes(3, prev.Bytes())
			}
			stmt.BindInt64(4, int64(prevIndex))
			stmt.BindInt64(5, int64(units))
			stmt.BindInt64(6, int64(publish))
		},
		nil,
	)
	return err
}

// AtxWithPrevious returns the ATX ID that has the given ATX ID as its previous ATX.
func AtxWithPrevious(db sql.Executor, prev types.ATXID, id types.NodeID) (types.ATXID, error) {
	var (
		atxid types.ATXID
		rows  int
		err   error
	)
	decode := func(s *sql.Statement) bool {
		s.ColumnBytes(0, atxid[:])
		return false
	}
	if prev == types.EmptyATXID {
		rows, err = db.Exec("SELECT atxid FROM posts WHERE pubkey = ?1 AND prev_atxid IS NULL;",
			func(s *sql.Statement) {
				s.BindBytes(1, id.Bytes())
			},
			decode,
		)
	} else {
		rows, err = db.Exec(`
		SELECT atxid FROM posts WHERE pubkey = ?1 AND prev_atxid = ?2;`,
			func(s *sql.Statement) {
				s.BindBytes(1, id.Bytes())
				s.BindBytes(2, prev.Bytes())
			},
			decode,
		)
	}
	if err != nil {
		return types.EmptyATXID, err
	}
	if rows == 0 {
		return types.EmptyATXID, sql.ErrNotFound
	}
	return atxid, nil
}

// Find 2 distinct merged ATXs (having the same marriage ATX) in the same epoch.
func MergeConflict(db sql.Executor, marriage types.ATXID, publish types.EpochID) ([]types.ATXID, error) {
	var ids []types.ATXID
	rows, err := db.Exec(`
		SELECT id FROM atxs WHERE marriage_atx = ?1 and epoch = ?2;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, marriage.Bytes())
			stmt.BindInt64(2, int64(publish))
		},
		func(stmt *sql.Statement) bool {
			var id types.ATXID
			stmt.ColumnBytes(0, id[:])
			ids = append(ids, id)
			return len(ids) < 2
		},
	)
	if err != nil {
		return nil, err
	}
	if rows != 2 {
		return nil, sql.ErrNotFound
	}
	return ids, nil
}
