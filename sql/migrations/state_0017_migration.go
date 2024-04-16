package migrations

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type atxInfo struct {
	atxID     types.ATXID
	nodeID    types.NodeID
	nonce     *types.VRFPostIndex
	prevATXID *types.ATXID
}

type atxUpdate struct {
	nonce     *types.VRFPostIndex
	prevATXID *types.ATXID
}

type migration0017 struct {
	logger         *zap.Logger
	nodeMap        map[types.NodeID]atxInfo
	pendingUpdates map[types.ATXID]atxUpdate
}

func New0017Migration(log *zap.Logger) *migration0017 {
	return &migration0017{
		logger:         log,
		nodeMap:        make(map[types.NodeID]atxInfo),
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
		if l := stmt.ColumnLen(3); l != 0 {
			var atx wire.ActivationTxV1
			if n, err := codec.DecodeFrom(stmt.ColumnReader(3), &atx); err != nil {
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
			atxID:     atxID,
			nodeID:    nodeID,
			nonce:     nonce,
			prevATXID: prevATXID,
		}); err != nil {
			ierr = fmt.Errorf("error processing ATX %s: %w", atxID, err)
			return false
		}
		return true
	}
	if _, err := db.Exec(`
		select a.id, a.pubkey, a.nonce, b.atx
		from atxs a
                inner join atx_blobs b on a.id = b.id
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
		upd.nonce = a.nonce
	}

	if upd.nonce != nil || upd.prevATXID != nil {
		m.pendingUpdates[a.atxID] = upd
	}

	m.nodeMap[a.nodeID] = a
	return nil
}

func (m *migration0017) findNonce(db sql.Executor, a atxInfo) (*types.VRFPostIndex, error) {
	if a.prevATXID != nil {
		upd, found := m.pendingUpdates[*a.prevATXID]
		if found && upd.nonce != nil {
			return upd.nonce, nil
		}
	} else {
		return nil, errors.New("no prev ATX and no nonce")
	}

	switch a1, found := m.nodeMap[a.nodeID]; {
	case found && a1.nonce == nil:
		panic("BUG: no nonce in the nodeMap entry")
	case !found || a1.atxID != *a.prevATXID:
		// nodeMap didn't give the correct result, we have to resort to
		// slow querying
		v, err := m.slowGetNonceByATXID(db, *a.prevATXID)
		if err != nil {
			return nil, err
		}
		return &v, nil
	default:
		return a1.nonce, nil
	}
}

func (m *migration0017) slowGetNonceByATXID(
	db sql.Executor,
	atxID types.ATXID,
) (types.VRFPostIndex, error) {
	upd, found := m.pendingUpdates[atxID]
	if found && upd.nonce != nil {
		return *upd.nonce, nil
	}

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

	var atx wire.ActivationTxV1
	if err := codec.Decode(blob, &atx); err != nil {
		return 0, fmt.Errorf("error decoding ATX blob: %w", err)
	}

	if atx.PrevATXID == types.EmptyATXID {
		return 0, errors.New("no prev ATX ID and no nonce")
	}

	return m.slowGetNonceByATXID(db, atx.PrevATXID)
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
	case upd.prevATXID == nil:
		panic("BUG: bad atxUpdate")
	case upd.nonce != nil && upd.prevATXID != nil:
		enc = func(stmt *sql.Statement) {
			stmt.BindBytes(1, (*upd.prevATXID)[:])
			stmt.BindInt64(2, int64(*upd.nonce))
			stmt.BindBytes(3, atxID[:])
		}
		sqlCmd = "update atxs set prev_id = ?1, nonce = ?2 where id = ?3"
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
