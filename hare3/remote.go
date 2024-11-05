package hare3

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type NodeService interface {
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
	svc       NodeService

	log *zap.Logger
}

func NewRemoteHare(config Config,
	nodeClock nodeClock,
	nodeService NodeService,
	oracle oracle,
	log *zap.Logger,
) *RemoteHare {
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

func (h *RemoteHare) Start(ctx context.Context) {
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
				h.onLayer(ctx, next)
			case <-ctx.Done():
				h.log.Info("remote hare exiting")
				return nil
			}
		}
		return nil
	})
}

func (h *RemoteHare) beacon(ctx context.Context, e types.EpochID) types.Beacon {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.beacons[e]
	if !ok {
		bcn, err := h.svc.Beacon(ctx, e)
		if err != nil {
			h.log.Error("error getting beacon", zap.Error(err))
			return types.EmptyBeacon
		}
		h.beacons[e] = bcn
		return bcn
	}

	return b
}

func (h *RemoteHare) onLayer(ctx context.Context, layer types.LayerID) {
	h.log.Debug("remote hare: on layer", zap.Int("layer", int(layer)))
	beacon := h.beacon(ctx, layer.GetEpoch())
	if beacon == types.EmptyBeacon {
		h.log.Debug("no beacon",
			zap.Uint32("epoch", layer.GetEpoch().Uint32()),
			zap.Uint32("lid", layer.Uint32()),
		)
		return
	}

	h.mu.Lock()
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
	h.log.Debug("registered layer", zap.Uint32("lid", layer.Uint32()))
	h.eg.Go(func() error {
		if err := h.run(ctx, s); err != nil {
			h.log.Warn("failed",
				zap.Uint32("lid", layer.Uint32()),
				zap.Error(err),
			)
			exitErrors.Inc()
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

func (h *RemoteHare) run(ctx context.Context, session *session) error {
	var (
		current = IterRound{Round: preround}
		start   = time.Now()
		active  bool
	)
	for i, signer := range session.signers {
		session.vrfs[i] = h.oracle.active(signer, session.beacon, session.lid, current)
		active = active || session.vrfs[i] != nil
	}
	activeLatency.Observe(time.Since(start).Seconds())

	walltime := h.nodeClock.LayerToTime(session.lid).Add(h.config.PreroundDelay)
	if active {
		h.log.Debug("active in preround. waiting for preround delay", zap.Uint32("lid", session.lid.Uint32()))
		select {
		case <-h.wallClock.After(walltime.Sub(h.wallClock.Now())):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	msgBytes, err := h.svc.GetHareMessage(ctx, session.lid, session.proto.IterRound)
	if err != nil && active {
		h.log.Error("get hare message on preround", zap.Error(err))
	} else {
		msg := &Message{}
		if err := codec.Decode(msgBytes, msg); err != nil {
			h.log.Error("decode remote hare message", zap.Error(err))
		} else {
			h.signPub(ctx, session, msg)
		}
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

				msgBytes, err := h.svc.GetHareMessage(ctx, session.lid, session.proto.IterRound)
				if msgBytes == nil && err == nil {
					// special case - no message to process, we're either too early or hare terminated.
					// do the onRound and then continue
					onRound(session.proto) // advance the protocol state before continuing
					continue
				}
				if err != nil {
					h.log.Error("get hare message", zap.Error(err))
					onRound(session.proto) // advance the protocol state before continuing
					continue
				}
				msg := &Message{}
				if err := codec.Decode(msgBytes, msg); err != nil {
					h.log.Error("decode remote hare message", zap.Error(err))
				}
				h.signPub(ctx, session, msg)
			}

			onRound(session.proto) // advance the protocol state before continuing
		case <-ctx.Done():
			return nil
		}
	}
}

func (h *RemoteHare) signPub(ctx context.Context, session *session, message *Message) {
	for i, vrf := range session.vrfs {
		if vrf == nil {
			continue
		}
		msg := *message
		msg.Layer = session.lid
		msg.Eligibility = *vrf
		msg.Sender = session.signers[i].NodeID()
		msg.Signature = session.signers[i].Sign(signing.HARE, msg.ToMetadata().ToBytes())
		if err := h.svc.Publish(ctx, h.config.ProtocolName, msg.ToBytes()); err != nil {
			h.log.Error("failed to publish", zap.Inline(&msg), zap.Error(err))
		}
	}
}

func onRound(p *protocol) {
	if p.Round == preround && p.Iter == 0 {
		p.Round = softlock
	} else if p.Round == notify {
		p.Round = hardlock
		p.Iter++
	} else {
		p.Round++
	}
}
