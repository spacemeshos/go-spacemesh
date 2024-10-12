package hare3

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"
)

type nodeService interface {
	GetHareMessage(ctx context.Context, layer types.LayerID, round IterRound) ([]byte, error)
	Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error)
	Publish(ctx context.Context, proto string, blob []byte) error
}

type RemoteHare struct {
	config    Config
	wallClock clockwork.Clock
	nodeClock nodeClock
	mu        sync.Mutex
	beacons   map[types.EpochID]types.Beacon
	signers   map[string]*signing.EdSigner
	oracle    *legacyOracle
	sessions  map[types.LayerID]*protocol
	eg        errgroup.Group
	ctx       context.Context
	svc       nodeService

	log *zap.Logger
}

// type remote
func NewRemoteHare(config Config, nodeClock nodeClock, nodeService nodeService, oracle oracle, log *zap.Logger) *RemoteHare {
	return &RemoteHare{
		config:    config,
		nodeClock: nodeClock,
		beacons:   make(map[types.EpochID]types.Beacon),
		signers:   make(map[string]*signing.EdSigner),
		oracle: &legacyOracle{
			log:    zap.NewNop(),
			oracle: oracle,
			config: DefaultConfig(),
		},

		sessions:  make(map[types.LayerID]*protocol),
		eg:        errgroup.Group{},
		ctx:       context.Background(),
		svc:       nodeService,
		log:       log,
		wallClock: clockwork.NewRealClock(),
	}
}

func (h *RemoteHare) Register(sig *signing.EdSigner) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.log.Info("registered signing key", log.ZShortStringer("id", sig.NodeID()))
	h.signers[string(sig.NodeID().Bytes())] = sig
}

func (h *RemoteHare) Start() {
	current := h.nodeClock.CurrentLayer() + 1
	enabled := max(current, h.config.EnableLayer, types.GetEffectiveGenesis()+1)
	disabled := types.LayerID(math.MaxUint32)
	h.log.Info("started",
		zap.Inline(&h.config),
		zap.Uint32("enabled", enabled.Uint32()),
		zap.Uint32("disabled", disabled.Uint32()),
	)
	h.eg.Go(func() error {
		h.log.Info("remote hare processing starting")
		for next := enabled; next < disabled; next++ {
			h.log.Info("remote hare processing layer", zap.Int("next", int(next)))
			select {
			case <-h.nodeClock.AwaitLayer(next):
				h.log.Debug("notified", zap.Uint32("layer", next.Uint32()))
				h.onLayer(next)
			case <-h.ctx.Done():
				h.log.Info("remote hare processing layer - context done")
				return nil
			}
		}
		return nil
	})
}

func (h *RemoteHare) beacon(e types.EpochID) types.Beacon {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.beacons[e]
	if !ok {
		bcn, err := h.svc.Beacon(context.Background(), e)
		if err != nil {
			h.log.Error("error getting beacon", zap.Error(err))
			return types.EmptyBeacon
		}
		h.beacons[e] = bcn
		return bcn
	}

	return b
}

func (h *RemoteHare) onLayer(layer types.LayerID) {
	h.log.Debug("remote hare: on layer", zap.Int("layer", int(layer)))
	beacon := h.beacon(layer.GetEpoch())
	if beacon == types.EmptyBeacon {
		h.log.Debug("no beacon",
			zap.Uint32("epoch", layer.GetEpoch().Uint32()),
			zap.Uint32("lid", layer.Uint32()),
			// zap.Error(err),
		)
		return
	}

	h.mu.Lock()
	// signer can't join mid session
	s := &session{
		lid:     layer,
		beacon:  beacon,
		signers: maps.Values(h.signers),
		vrfs:    make([]*types.HareEligibility, len(h.signers)),
		proto:   newProtocol(h.config.CommitteeFor(layer)/2 + 1),
	}
	h.sessions[layer] = s.proto
	h.mu.Unlock()

	sessionStart.Inc()
	// h.tracer.OnStart(layer)
	h.log.Debug("registered layer", zap.Uint32("lid", layer.Uint32()))
	h.eg.Go(func() error {
		if err := h.run(s); err != nil {
			h.log.Warn("failed",
				zap.Uint32("lid", layer.Uint32()),
				zap.Error(err),
			)
			exitErrors.Inc()
			// if terminated successfully it will notify block generator
			// and it will have to CompleteHare
		} else {
			h.log.Debug("terminated",
				zap.Uint32("lid", layer.Uint32()),
			)
		}
		h.mu.Lock()
		delete(h.sessions, layer)
		h.mu.Unlock()
		sessionTerminated.Inc()
		return nil
	})
}

func (h *RemoteHare) selectProposals(session *session) error {
	return nil
}

func (h *RemoteHare) run(session *session) error {
	var (
		current = IterRound{Round: preround}
		start   = time.Now()
		active  bool
	)
	for i := range session.signers {
		session.vrfs[i] = h.oracle.active(session.signers[i], session.beacon, session.lid, current)
		active = active || session.vrfs[i] != nil
	}
	activeLatency.Observe(time.Since(start).Seconds())

	walltime := h.nodeClock.LayerToTime(session.lid).Add(h.config.PreroundDelay)
	if active {
		h.log.Debug("active in preround. waiting for preround delay", zap.Uint32("lid", session.lid.Uint32()))
		// initial set is not needed if node is not active in preround
		select {
		case <-h.wallClock.After(walltime.Sub(h.wallClock.Now())):
		case <-h.ctx.Done():
			return h.ctx.Err()
		}
		start := time.Now()
		// TODO this still has the prerequisite of handling the proposals construction correctly
		proposalsLatency.Observe(time.Since(start).Seconds())
	}
	onRound(session.proto)
	for {
		if session.proto.IterRound.Iter >= h.config.IterationsLimit {
			return nil
		}

		walltime = walltime.Add(h.config.RoundDuration)
		current = session.proto.IterRound
		start = time.Now()
		active := false

		for i := range session.signers {
			if current.IsMessageRound() {
				session.vrfs[i] = h.oracle.active(session.signers[i], session.beacon, session.lid, current)
				active = active || (session.vrfs[i] != nil)
			} else {
				session.vrfs[i] = nil
			}
		}
		activeLatency.Observe(time.Since(start).Seconds())

		select {
		case <-h.wallClock.After(walltime.Sub(h.wallClock.Now())):
			if active {
				h.log.Debug("execute round",
					zap.Uint32("lid", session.lid.Uint32()),
					zap.Uint8("iter", session.proto.Iter), zap.Stringer("round", session.proto.Round),
					zap.Bool("active", active),
				)

				msgBytes, err := h.svc.GetHareMessage(context.Background(), session.lid, session.proto.IterRound)
				if err != nil {
					h.log.Error("get hare message", zap.Error(err))
					onRound(session.proto) // advance the protocol state before continuing
					continue
				}
				msg := &Message{}
				if err := codec.Decode(msgBytes, msg); err != nil {
					h.log.Error("decode remote hare message", zap.Error(err))
				}
				h.signPub(session, msg)
			}

			onRound(session.proto) // advance the protocol state before continuing
		case <-h.ctx.Done():
			return nil
		}
	}
}

func (h *RemoteHare) signPub(session *session, message *Message) {
	for i, vrf := range session.vrfs {
		if vrf == nil {
			continue
		}
		msg := *message
		msg.Layer = session.lid
		msg.Eligibility = *vrf
		msg.Sender = session.signers[i].NodeID()
		msg.Signature = session.signers[i].Sign(signing.HARE, msg.ToMetadata().ToBytes())
		if err := h.svc.Publish(h.ctx, h.config.ProtocolName, msg.ToBytes()); err != nil {
			h.log.Error("failed to publish", zap.Inline(&msg), zap.Error(err))
		}
	}
}

func onRound(p *protocol) {
	if p.Round == preround && p.Iter == 0 {
		// skips hardlock unlike softlock in the paper.
		// this makes no practical difference from correctness.
		// but allows to simplify assignment in validValues
		p.Round = softlock
	} else if p.Round == notify {
		p.Round = hardlock
		p.Iter++
	} else {
		p.Round++
	}
}
