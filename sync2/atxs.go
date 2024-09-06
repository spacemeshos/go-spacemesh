package sync2

import (
	"context"

	"github.com/libp2p/go-libp2p/core/host"

	smtypes "github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/dbsync"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
	"github.com/spacemeshos/go-spacemesh/system"
	"go.uber.org/zap"
)

const (
	oldAtxProto    = "sync2/old-atx"
	curAtxProto    = "sync2/cur-atx"
	oldAtxMaxDepth = 16
	curAtxMaxDepth = 24
)

type Fetcher interface {
	system.AtxFetcher
	Host() host.Host
	Peers() *peers.Peers
	RegisterPeerHash(peer p2p.Peer, hash smtypes.Hash32)
}

type layerTicker interface {
	CurrentLayer() smtypes.LayerID
}

func AtxHandler(f Fetcher) multipeer.SyncKeyHandler {
	return func(ctx context.Context, k types.Ordered, peer p2p.Peer) error {
		var id smtypes.ATXID
		copy(id[:], k.(types.KeyBytes))
		f.RegisterPeerHash(peer, id.Hash32())
		if err := f.GetAtxs(ctx, []smtypes.ATXID{id}); err != nil {
			return err
		}
		return nil
	}
}

func atxsTable(epochFilter string, curEpoch smtypes.EpochID) *dbsync.SyncedTable {
	return &dbsync.SyncedTable{
		TableName:       "atxs",
		IDColumn:        "id",
		TimestampColumn: "received",
		Filter:          dbsync.MustParseSQLExpr(epochFilter),
		Binder: func(s *sql.Statement) {
			s.BindInt64(1, int64(curEpoch))
		},
	}
}

func newAtxSyncer(
	logger *zap.Logger,
	cfg Config,
	db sql.StateDatabase,
	f Fetcher,
	ticker layerTicker,
	epochFilter string,
	maxDepth int,
	proto string,
) *P2PHashSync {
	// TODO: handle epoch switch
	curEpoch := ticker.CurrentLayer().GetEpoch()
	curSet := dbsync.NewDBSet(db, atxsTable(epochFilter, curEpoch), 32, curAtxMaxDepth)
	return NewP2PHashSync(logger, f.Host(), curSet, 32, maxDepth, proto, f.Peers(), AtxHandler(f), cfg)
}

func NewCurAtxSyncer(
	logger *zap.Logger,
	cfg Config,
	db sql.StateDatabase,
	f Fetcher,
	ticker layerTicker,
) *P2PHashSync {
	return newAtxSyncer(logger, cfg, db, f, ticker, "epoch = ?", curAtxMaxDepth, curAtxProto)
}

func NewOldAtxSyncer(
	logger *zap.Logger,
	cfg Config,
	db sql.StateDatabase,
	f Fetcher,
	ticker layerTicker,
) *P2PHashSync {
	return newAtxSyncer(logger, cfg, db, f, ticker, "epoch < ?", oldAtxMaxDepth, oldAtxProto)
}

// TODO: test
// TODO: per-round SQL transactions
