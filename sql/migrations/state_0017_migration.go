package migrations

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type atxInfo struct {
	atxID          types.ATXID
	nodeID         types.NodeID
	isEquivocating bool
	nonce          *types.VRFPostIndex
	prevATXID      *types.ATXID
}

type atxUpdate struct {
	nonce     *types.VRFPostIndex
	prevATXID *types.ATXID
}

type migration0017 struct {
	logger         *zap.Logger
	nodeMap        map[types.NodeID]atxInfo
	atxMap         map[types.ATXID]types.VRFPostIndex
	danglingMap    map[types.ATXID][]atxInfo
	pendingUpdates map[types.ATXID]atxUpdate
}

func New0017Migration(log *zap.Logger) *migration0017 {
	return &migration0017{
		logger:         log,
		nodeMap:        make(map[types.NodeID]atxInfo),
		atxMap:         make(map[types.ATXID]types.VRFPostIndex),
		danglingMap:    make(map[types.ATXID][]atxInfo),
		pendingUpdates: make(map[types.ATXID]atxUpdate),
	}
}

func (*migration0017) Name() string {
	return "always set ATX nonce"
}

func (*migration0017) Order() int {
	return 17
}

func (*migration0017) Rollback() error {
	// handled by the DB itself
	return nil
}

func (m *migration0017) Apply(db sql.Executor) error {
	maxEpoch, err := getMaxEpoch(db)
	if err != nil {
		return err
	}
	for epoch := types.EpochID(1); epoch <= maxEpoch; epoch++ {
		if err := m.processEpoch(db, epoch); err != nil {
			return fmt.Errorf("error processing epoch %d: %w", epoch, err)
		}
	}
	if len(m.danglingMap) != 0 {
		return errors.New("dangling entries remain")
	}
	return nil
}

func (m *migration0017) processEpoch(db sql.Executor, epoch types.EpochID) error {
	m.logger.Info("processing epoch", zap.Stringer("epoch", epoch))
	var ierr error
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
	}
	dec := func(stmt *sql.Statement) bool {
		var (
			atxID     types.ATXID
			nodeID    types.NodeID
			nonce     *types.VRFPostIndex
			prevATXID *types.ATXID
		)
		stmt.ColumnBytes(0, atxID[:])
		stmt.ColumnBytes(1, nodeID[:])
		if !sql.IsNull(stmt, 2) {
			v := types.VRFPostIndex(stmt.ColumnInt64(2))
			nonce = &v
		}
		isEquivocating := stmt.ColumnInt(3) != 0
		if l := stmt.ColumnLen(4); l != 0 {
			var atx types.ActivationTx
			if n, err := codec.DecodeFrom(stmt.ColumnReader(4), &atx); err != nil {
				ierr = fmt.Errorf("error decoding ATX %s: %w", atxID, err)
				return false
			} else if n != l {
				ierr = fmt.Errorf("error decoding ATX %s: bad blob length", atxID)
				return false
			}
			if atx.PrevATXID != types.EmptyATXID {
				prevATXID = &atx.PrevATXID
			}
		}
		if err := m.processATX(db, atxInfo{
			atxID:          atxID,
			nodeID:         nodeID,
			isEquivocating: isEquivocating,
			nonce:          nonce,
			prevATXID:      prevATXID,
		}); err != nil {
			ierr = fmt.Errorf("error processing ATX %s: %w", atxID, err)
			return false
		}
		return true
	}
	if _, err := db.Exec(`
		select a.id, a.pubkey, a.nonce,
		  iif(eq.pubkey is null, 0, 1) as is_equivocating,
		  iif(a.nonce is null, b.atx, null) as atx
		from atxs a
                inner join atx_blobs b on a.id = b.id
		left join (
		  select pubkey from atxs where epoch = ?1 group by pubkey having count(id) > 1
		) eq on a.pubkey = eq.pubkey
		where epoch = ?1`, enc, dec); err != nil {
		return fmt.Errorf("error processing epoch %d: %w", epoch, err)
	}
	if ierr != nil {
		return fmt.Errorf("error processing epoch %d: %w", epoch, ierr)
	}

	return m.applyPendingUpdates(db)
}

func (m *migration0017) processATX(db sql.Executor, a atxInfo) error {
	upd := atxUpdate{prevATXID: a.prevATXID}
	if a.nonce == nil {
		var err error
		a.nonce, err = m.findNonce(db, a)
		if err != nil {
			return err
		}
		if a.nonce == nil {
			if !a.isEquivocating {
				// This ATX is non-equivocating and has no nonce, so its
				// previous ATX must have been encountered when processing
				// a previous epoch
				return fmt.Errorf("non-equivocating ATX with dangling prev ATX ID %s",
					*a.prevATXID)
			}
			m.danglingMap[*a.prevATXID] = append(m.danglingMap[*a.prevATXID], a)
			return nil
		}
		upd.nonce = a.nonce
	}

	if upd.nonce != nil || upd.prevATXID != nil {
		m.pendingUpdates[a.atxID] = upd
	}

	if err := m.resolveDangling(db, a.atxID, *a.nonce, nil); err != nil {
		return err
	}

	m.updateMaps(a)
	return nil
}

func (m *migration0017) updateMaps(
	a atxInfo,
) {
	if a.isEquivocating {
		// No reliable way to map node ID to a nonce in the next epoch,
		// but let's also keep previous ATX ID in the map
		if a1, found := m.nodeMap[a.nodeID]; found {
			m.atxMap[a1.atxID] = *a1.nonce
			delete(m.nodeMap, a.nodeID)
		}
		m.atxMap[a.atxID] = *a.nonce
	} else {
		m.nodeMap[a.nodeID] = a
	}
}

func (m *migration0017) resolveDangling(
	db sql.Executor,
	atxID types.ATXID,
	nonce types.VRFPostIndex,
	visited map[types.ATXID]struct{},
) error {
	if visited == nil {
		visited = make(map[types.ATXID]struct{})
	}
	if _, found := visited[atxID]; found {
		return errors.New("dangling ATX cycle detected")
	}
	for _, a := range m.danglingMap[atxID] {
		a.nonce = &nonce
		m.updateMaps(a)
		m.pendingUpdates[a.atxID] = atxUpdate{
			nonce:     a.nonce,
			prevATXID: a.prevATXID,
		}
		if err := m.resolveDangling(db, a.atxID, nonce, visited); err != nil {
			return err
		}
	}
	delete(m.danglingMap, atxID)
	return nil
}

func (m *migration0017) findNonce(db sql.Executor, a atxInfo) (*types.VRFPostIndex, error) {
	var prevNonce *types.VRFPostIndex
	if a.prevATXID != nil {
		v, found := m.atxMap[*a.prevATXID]
		if found {
			prevNonce = &v
		} else {
			upd, found := m.pendingUpdates[*a.prevATXID]
			if found && upd.nonce != nil {
				prevNonce = upd.nonce
			}
		}
	} else {
		return nil, errors.New("no prev ATX and no nonce")
	}

	switch a1, found := m.nodeMap[a.nodeID]; {
	case !found:
		return prevNonce, nil
	case a.prevATXID == nil:
		return nil, errors.New("no prev ATX but prev nonce found")
	case a1.atxID != *a.prevATXID:
		// nodeMap didn't give the correct result, we have to resort to
		// slow querying
		v, err := getNonce(db, *a.prevATXID)
		if err != nil {
			return nil, err
		}
		return &v, nil
	case prevNonce != nil:
		return nil, errors.New("equivocating nonce found via node ID map")
	case a1.nonce == nil:
		panic("BUG: no nonce in the nodeMap entry")
	default:
		return a1.nonce, nil
	}
}

func (m *migration0017) applyPendingUpdates(db sql.Executor) error {
	total := len(m.pendingUpdates)
	atxIDs := maps.Keys(m.pendingUpdates)
	slices.SortFunc(atxIDs, func(a, b types.ATXID) int {
		return bytes.Compare(a[:], b[:])
	})
	for n, atxID := range atxIDs {
		if (n+1)%100000 == 1 || n == total-1 {
			m.logger.Info("updating atxs", zap.Int("current", n+1), zap.Int("total", total))
		}
		if err := updateATX(db, atxID, m.pendingUpdates[atxID]); err != nil {
			return err
		}
	}
	maps.Clear(m.pendingUpdates)
	return nil
}

func updateATX(db sql.Executor, atxID types.ATXID, upd atxUpdate) error {
	var (
		sqlCmd string
		enc    sql.Encoder
	)
	switch {
	case upd.nonce == nil && upd.prevATXID == nil:
		panic("BUG: bad atxUpdate")
	case upd.nonce != nil && upd.prevATXID != nil:
		enc = func(stmt *sql.Statement) {
			stmt.BindBytes(1, (*upd.prevATXID)[:])
			stmt.BindInt64(2, int64(*upd.nonce))
			stmt.BindBytes(3, atxID[:])
		}
		sqlCmd = "update atxs set prev_id = ?1, nonce = ?2 where id = ?3"
	case upd.nonce != nil:
		enc = func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(*upd.nonce))
			stmt.BindBytes(2, atxID[:])
		}
		sqlCmd = "update atxs set nonce = ?1 where id = ?2"
	default:
		enc = func(stmt *sql.Statement) {
			stmt.BindBytes(1, (*upd.prevATXID)[:])
			stmt.BindBytes(2, atxID[:])
		}
		sqlCmd = "update atxs set prev_id = ?1 where id = ?2"
	}
	if _, err := db.Exec(sqlCmd, enc, nil); err != nil {
		return fmt.Errorf("error updating ATX: %w", err)
	}
	return nil
}

func getMaxEpoch(db sql.Executor) (epoch types.EpochID, err error) {
	dec := func(stmt *sql.Statement) bool {
		epoch = types.EpochID(stmt.ColumnInt64(0))
		return true
	}
	if rows, err := db.Exec("select max(epoch) from atxs", nil, dec); err != nil {
		return 0, fmt.Errorf("error getting max epoch: %w", err)
	} else if rows == 0 {
		return 1, nil
	}
	return epoch, nil
}

func getNonce(db sql.Executor, atxID types.ATXID) (types.VRFPostIndex, error) {
	var nonce *types.VRFPostIndex
	var blob []byte
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, atxID[:])
	}
	dec := func(stmt *sql.Statement) bool {
		if !sql.IsNull(stmt, 0) {
			v := types.VRFPostIndex(stmt.ColumnInt64(0))
			nonce = &v
		}
		if stmt.ColumnLen(1) != 0 {
			blob = make([]byte, stmt.ColumnLen(1))
			stmt.ColumnBytes(1, blob)
		}
		return true
	}
	if rows, err := db.Exec(`
		select a.nonce, b.atx
		  from atxs a
		  inner join atx_blobs b on a.id = b.id
		  where a.id = ?1`, enc, dec); err != nil {
		return 0, fmt.Errorf("error getting nonce: %w", err)
	} else if rows == 0 {
		return 0, sql.ErrNotFound
	}

	if nonce != nil {
		return *nonce, nil
	}

	if len(blob) == 0 {
		return 0, errors.New("missing ATX blob")
	}

	var atx types.ActivationTx
	if err := codec.Decode(blob, &atx); err != nil {
		return 0, fmt.Errorf("error decoding ATX blob: %w", err)
	}

	if atx.PrevATXID == types.EmptyATXID {
		return 0, errors.New("no prev ATX ID and no nonce")
	}

	return getNonce(db, atx.PrevATXID)
}
