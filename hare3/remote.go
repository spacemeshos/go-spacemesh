package hare3

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

type RemoteHare struct {
	eligibility *eligibility.Oracle
	nodeClock   nodeClock
	mu          sync.Mutex
	beacons     map[types.EpochID]types.Beacon
	signers     map[string]*signing.EdSigner
	oracle      *legacyOracle
	sessions    map[types.LayerID]*protocol

	log *zap.Logger
}

func NewRemoteHare() *RemoteHare {
	return &RemoteHare{}
}

func (h *RemoteHare) Register(sig *signing.EdSigner) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.log.Info("registered signing key", log.ZShortStringer("id", sig.NodeID()))
	h.signers[string(sig.NodeID().Bytes())] = sig
}

func (h *RemoteHare) Start() {
	current := h.nodeClock.CurrentLayer() + 1
	enableLayer = 0
	enabled := max(current, enableLayer /* h.config.EnableLayer*/, types.GetEffectiveGenesis()+1)
	disabled := types.LayerID(math.MaxUint32)
	if h.config.DisableLayer > 0 {
		disabled = h.config.DisableLayer
	}
	h.log.Info("started",
		// zap.Inline(&h.config),
		zap.Uint32("enabled", enabled.Uint32()),
		zap.Uint32("disabled", disabled.Uint32()),
	)
	h.eg.Go(func() error {
		for next := enabled; next < disabled; next++ {
			select {
			case <-h.nodeClock.AwaitLayer(next):
				h.log.Debug("notified", zap.Uint32("layer", next.Uint32()))
				h.onLayer(next)
				h.cleanMessageCache(next - 1)
			case <-h.ctx.Done():
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
		return types.EmptyBeacon
	}

	return b
}

func (h *RemoteHare) onLayer(layer types.LayerID) {
	beacon, err := h.beacon(layer.GetEpoch())
	if err != nil || beacon == types.EmptyBeacon {
		h.log.Debug("no beacon",
			zap.Uint32("epoch", layer.GetEpoch().Uint32()),
			zap.Uint32("lid", layer.Uint32()),
			zap.Error(err),
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
	h.tracer.OnStart(layer)
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
		session.proto.OnInitial(h.selectProposals(session))
		proposalsLatency.Observe(time.Since(start).Seconds())
	}
	if err := h.onOutput(session, current, session.proto.Next()); err != nil {
		return err
	}
	result := false
	for {
		walltime = walltime.Add(h.config.RoundDuration)
		current = session.proto.IterRound
		start = time.Now()

		for i := range session.signers {
			if current.IsMessageRound() {
				session.vrfs[i] = h.oracle.active(session.signers[i], session.beacon, session.lid, current)
			} else {
				session.vrfs[i] = nil
			}
		}
		h.tracer.OnActive(session.vrfs)
		activeLatency.Observe(time.Since(start).Seconds())

		select {
		case <-h.wallClock.After(walltime.Sub(h.wallClock.Now())):
			h.log.Debug("execute round",
				zap.Uint32("lid", session.lid.Uint32()),
				zap.Uint8("iter", session.proto.Iter), zap.Stringer("round", session.proto.Round),
				zap.Bool("active", active),
			)
			out := session.proto.Next()
			if out.result != nil {
				result = true
			}
			if err := h.onOutput(session, current, out); err != nil {
				return err
			}
			// we are logginng stats 1 network delay after new iteration start
			// so that we can receive notify messages from previous iteration
			if session.proto.Round == softlock && h.config.LogStats {
				h.log.Debug("stats", zap.Uint32("lid", session.lid.Uint32()), zap.Inline(session.proto.Stats()))
			}
			if out.terminated {
				if !result {
					return errors.New("terminated without result")
				}
				return nil
			}
			if current.Iter == h.config.IterationsLimit {
				return fmt.Errorf("hare failed to reach consensus in %d iterations", h.config.IterationsLimit)
			}
		case <-h.ctx.Done():
			return nil
		}
	}
}
