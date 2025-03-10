package hare3

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type beaconService interface {
	Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error)
}

type RemoteHare struct {
	config    Config
	wallClock clockwork.Clock
	nodeClock nodeClock
	mu        sync.Mutex
	signers   map[string]*signing.EdSigner
	oracle    *legacyOracle
	sessions  map[types.LayerID]*protocol
	eg        errgroup.Group
	svc       NodeService
	beaconSvc beaconService
	certifier certifier

	log *zap.Logger
}

func NewRemoteHare(config Config,
	nodeClock nodeClock,
	nodeService NodeService,
	beaconService beaconService,
	oracle oracle,
	certifier certifier,
	log *zap.Logger,
) *RemoteHare {
	return &RemoteHare{
		config:    config,
		nodeClock: nodeClock,
		signers:   make(map[string]*signing.EdSigner),
		oracle: &legacyOracle{
			log:    log,
			oracle: oracle,
			config: config,
		},
		certifier: certifier,

		sessions:  make(map[types.LayerID]*protocol),
		eg:        errgroup.Group{},
		svc:       nodeService,
		beaconSvc: beaconService,
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

func (h *RemoteHare) onLayer(ctx context.Context, layer types.LayerID) {
	h.log.Debug("remote hare: on layer", zap.Int("layer", int(layer)))

	beacon, err := h.beaconSvc.Beacon(ctx, layer.GetEpoch())
	if err != nil {
		h.log.Error("error getting beacon", zap.Error(err))
		return
	}

	h.mu.Lock()
	s := &session{
		lid:     layer,
		beacon:  beacon,
		signers: maps.Values(h.signers),
		vrfs:    make([]*types.HareEligibility, len(h.signers)),
		proto:   newProtocol(h.config.CommitteeFor(layer)/2+1, h.log.Named("proto")),
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

func (h *RemoteHare) certify(ctx context.Context, session *session, blockID types.BlockID) {
	for _, signer := range session.signers {
		err := h.certifier.CertifyBlock(ctx, signer, session.lid, blockID, session.beacon)
		if err != nil {
			// there isn't any handling that the caller could do so we log and return nil
			h.log.Warn(
				"failed to certify block",
				zap.Error(err),
				zap.Uint8("iter", session.proto.Iter),
				zap.Uint32("layer", session.lid.Uint32()),
				zap.Stringer("blockID", blockID),
				zap.Stringer("round", session.proto.Round),
				log.ZShortStringer("smesherID", signer.NodeID()),
			)
		}
	}
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
		body, err := h.svc.HareRoundTemplate(ctx, session.lid, session.proto.IterRound)
		if err != nil {
			h.log.Error("failed to get hare round template on preround", zap.Error(err))
		} else if body == nil {
			// do nothing, there's no message to process
		} else {
			msg := Message{
				Body: *body,
			}
			h.signPub(ctx, session, &msg)
		}
	}

	onRound(session.proto)
	certified := false
	for {
		if certified && session.proto.Round == hardlock {
			// The full iteration after hare converged passed.
			// It can now terminate.
			h.log.Debug(
				"hare terminated",
				zap.Uint8("iter", session.proto.Iter),
				zap.Stringer("round", session.proto.Round),
				zap.Uint32("layer", session.lid.Uint32()),
			)
			return nil
		}
		if !certified && session.proto.Iter > 0 {
			// Check if hare already converged and a block was produced.
			// If yes - certify it and quit.
			blockID, err := h.svc.BlockID(ctx, session.lid)
			switch {
			case err != nil:
				h.log.Debug("couldn't fetch block ID", zap.Uint32("layer", session.lid.Uint32()))
			case blockID == types.EmptyBlockID:
				h.log.Debug("hare has not converged yet", zap.Uint32("layer", session.lid.Uint32()))
			default:
				h.log.Debug(
					"hare converged",
					zap.Stringer("blockID", blockID),
					zap.Uint8("iter", session.proto.Iter),
					zap.Stringer("round", session.proto.Round),
					zap.Uint32("layer", session.lid.Uint32()),
				)
				h.certify(ctx, session, blockID)
				certified = true
				// The hare converged and block was produced.
				// However, we continue to participate in the protocol till
				// the end of the current iteration.
			}
		}
		if session.proto.Iter >= h.config.IterationsLimit {
			return fmt.Errorf("hare failed to reach consensus in %d iterations", h.config.IterationsLimit)
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

				body, err := h.svc.HareRoundTemplate(ctx, session.lid, session.proto.IterRound)
				if body == nil && err == nil {
					// special case - no message to process, we're either too early or hare terminated.
					// do the onRound and then continue
					onRound(session.proto) // advance the protocol state before continuing
					continue
				}
				if err != nil {
					h.log.Error("getting hare round template", zap.Error(err))
					onRound(session.proto) // advance the protocol state before continuing
					continue
				}
				msg := &Message{Body: *body}
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
		h.log.Debug("publishing hare message", zap.Stringer("beacon", session.beacon), zap.Inline(&msg))
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
