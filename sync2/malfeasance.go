package sync2

import (
	"context"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/dbset"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
)

type MalfeasanceHandler struct {
	logger *zap.Logger
	f      Fetcher
	clock  clockwork.Clock
	cfg    Config
}

var (
	_ multipeer.SyncKeyHandler = &MalfeasanceHandler{}
	_ Handler[types.NodeID]    = &MalfeasanceHandler{}
)

func NewMalfeasanceHandler(
	logger *zap.Logger,
	f Fetcher,
	cfg Config,
	clock clockwork.Clock,
) *MalfeasanceHandler {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &MalfeasanceHandler{
		f:      f,
		logger: logger,
		clock:  clock,
		cfg:    cfg,
	}
}

func (h *MalfeasanceHandler) Register(peer p2p.Peer, k rangesync.KeyBytes) types.NodeID {
	id := types.BytesToNodeID(k)
	h.f.RegisterPeerHashes(peer, []types.Hash32{types.Hash32(id)})
	return id
}

func (h *MalfeasanceHandler) Get(ctx context.Context, ids []types.NodeID, callback func(types.NodeID, error)) error {
	return h.f.GetMalfeasanceProofsWithCallback(ctx, ids, callback)
}

func (h *MalfeasanceHandler) Commit(
	ctx context.Context,
	peer p2p.Peer,
	base rangesync.OrderedSet,
	received rangesync.SeqResult,
) error {
	h.logger.Debug("begin malfeasance commit")
	defer h.logger.Debug("end malfeasance commit")
	cs, err := NewCommitState(h.logger, h, h.clock, peer, base, received, h.cfg)
	if err != nil {
		return err
	}
	return cs.Commit(ctx)
}

func identitiesTable() *sqlstore.SyncedTable {
	return &sqlstore.SyncedTable{
		TableName: "identities",
		IDColumn:  "pubkey",
	}
}

func NewMalfeasanceSyncer(
	logger *zap.Logger,
	d *rangesync.Dispatcher,
	name string,
	cfg Config,
	db sql.Database,
	f Fetcher,
	peers *peers.Peers,
	enableActiveSync bool,
) (*P2PHashSync, error) {
	curSet := dbset.NewDBSet(db, identitiesTable(), 32, int(cfg.MaxDepth))
	handler := NewMalfeasanceHandler(logger, f, cfg, nil)
	return NewP2PHashSync(logger, d, name, curSet, 32, peers, handler, cfg, enableActiveSync)
}
