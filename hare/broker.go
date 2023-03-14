package hare

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/system"
)

const inboxCapacity = 1024 // inbox size per instance

type validator interface {
	Validate(context.Context, *Msg) bool
	ValidateEligibilityGossip(context.Context, *types.HareEligibilityGossip) bool
}

// Broker is the dispatcher of incoming Hare messages.
// The broker validates that the sender is eligible and active and forwards the message to the corresponding outbox.
type Broker struct {
	log.Log
	mu sync.RWMutex

	msh             mesh
	pubKeyExtractor *signing.PubKeyExtractor
	roleValidator   validator    // provides eligibility validation
	stateQuerier    stateQuerier // provides activeness check
	nodeSyncState   system.SyncStateProvider
	mchOut          chan<- *types.MalfeasanceGossip
	outbox          map[uint32]chan any
	latestLayer     types.LayerID // the latest layer to attempt register (successfully or unsuccessfully)

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func newBroker(
	msh mesh,
	pubKeyExtractor *signing.PubKeyExtractor,
	roleValidator validator,
	stateQuerier stateQuerier,
	syncState system.SyncStateProvider,
	mch chan<- *types.MalfeasanceGossip,
	limit int,
	log log.Log,
) *Broker {
	b := &Broker{
		Log:             log,
		msh:             msh,
		pubKeyExtractor: pubKeyExtractor,
		roleValidator:   roleValidator,
		stateQuerier:    stateQuerier,
		nodeSyncState:   syncState,
		mchOut:          mch,
		outbox:          make(map[uint32]chan any),
		latestLayer:     types.GetEffectiveGenesis(),
	}
	return b
}

// Start listening to Hare messages (non-blocking).
func (b *Broker) Start(ctx context.Context) {
	b.once.Do(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.ctx, b.cancel = context.WithCancel(ctx)
	})
}

var errClosed = errors.New("closed")

// HandleMessage separate listener routine that receives gossip messages and adds them to the priority queue.
func (b *Broker) HandleMessage(ctx context.Context, _ p2p.Peer, msg []byte)
pubsub.ValidationResult {
	// We lock and unlock here to make sure we see b.ctx if it has been set.
	// The go memory model (See - https://go.dev/ref/mem#locks) says that:
	//
	// For any sync.Mutex or sync.RWMutex variable l and n < m, call n of
	// l.Unlock() is synchronized before call m of l.Lock() returns.
	//
	// Because b.ctx is set in Start(), by a different go-routine to the one
	// that calls this function, in order to ensure that we can see b.ctx's
	// value we need to ensure that the setting of b.ctx is synchroized before
	// our access to b.ctx here.
	//
	// Assuming that b.ctx has been set, or more precisely that the call to
	// b.mu.Lock() in Start() has already happened, we can ensure that we see
	// the result of setting b.ctx in Start() here by calling b.mu.Lock(),
	// since that will ensure that the call to b.mu.Unlock() in start is
	// synchroized before the call of b.mu.Lock() here.
	b.mu.Lock()
	//nolint:staticcheck
	//lint:ignore SA2001 See above comment, this seems legit.
	b.mu.Unlock()
	select {
	case <-ctx.Done():
		return pubsub.ValidationIgnore
	case <-b.ctx.Done():
		return pubsub.ValidationIgnore
	default:
		if err := b.handleMessage(ctx, msg); err != nil {
			return pubsub.ValidationIgnore
		}
	}
	return pubsub.ValidationAccept
}

func (b *Broker) handleMessage(ctx context.Context, msg []byte) error {
	h := types.CalcMessageHash12(msg, pubsub.HareProtocol)
	b.mu.RLock()
	logger := b.WithContext(ctx).WithFields(log.Stringer("latest_layer", b.latestLayer), h)
	b.mu.RUnlock()
	hareMsg, err := MessageFromBuffer(msg)
	if err != nil {
		logger.With().Error("failed to build message", h, log.Err(err))
		return err
	}
	logger = logger.WithFields(log.Inline(&hareMsg))

	if hareMsg.InnerMsg == nil {
		logger.With().Warning("hare msg missing inner msg", log.Err(errNilInner))
		return errNilInner
	}

	logger.Debug("broker received hare message")

	msgLayer := hareMsg.Layer

	if !(b.nodeSyncState.IsSynced(ctx) && b.nodeSyncState.IsBeaconSynced(msgLayer.GetEpoch())) {
		err := fmt.Errorf("broker rejecting message because not synced")
		logger.With().Debug("rejecting message", log.Err(err))
		return err
	}
	b.mu.Lock()
	out, exist := b.outbox[msgLayer.Uint32()]
	if !exist {
		logger.With().Debug(
			"broker received a message to a consensus process that is not registered",
			msgLayer.Field(),
			log.Stringer("msg_type", hareMsg.InnerMsg.Type),
		)

		// We consider messages for the next layer to be early and we process
		// them, all other messages we ignore. We don't impose the outbox
		// capacity at this point since this is an externally initiated event
		// and at most it can cause us to exceed our capacity by one slot.
		if msgLayer == b.latestLayer.Add(1) {
			out = make(chan any, inboxCapacity)
			b.outbox[msgLayer.Uint32()] = out
		} else {
			b.mu.Unlock()
			// This error is never inspected except to determine if it is not nil.
			return fmt.Errorf(
				"ignoring message to unregistered consensus process, latestLayer: %v, messageLayer: %v, messageType: %v",
				b.latestLayer,
				msgLayer.Uint32(),
				hareMsg.InnerMsg.Type,
			)
		}
	}
	b.mu.Unlock()

	nodeId, err := b.pubKeyExtractor.ExtractNodeID(signing.HARE, hareMsg.SignedBytes(), hareMsg.Signature)
	if err != nil {
		logger.With().Error("failed to extract public key",
			log.Err(err),
			log.Int("sig_len", len(hareMsg.Signature)))
		return fmt.Errorf("extract ed25519 pubkey: %w", err)
	}
	// create msg
	iMsg, err := newMsg(ctx, b.Log, nodeId, hareMsg, b.stateQuerier)
	if err != nil {
		logger.With().Warning("message validation failed: could not construct msg", log.Err(err))
		return err
	}

	// validate msg
	if !b.roleValidator.Validate(ctx, iMsg) {
		logger.Warning("message validation failed: eligibility validator returned false")
		return errors.New("not eligible")
	}

	// validation passed, report
	logger.With().Debug("broker reported hare message as valid")

	nodeID := types.BytesToNodeID(iMsg.PubKey.Bytes())
	if proof, err := b.msh.GetMalfeasanceProof(nodeID); err != nil && !errors.Is(err, sql.ErrNotFound) {
		logger.With().Error("failed to check malicious identity",
			log.Stringer("smesher", nodeID),
			log.Err(err),
		)
		return err
	} else if proof != nil {
		// when hare receives a hare gossip from a malicious identity,
		// - gossip its malfeasance + eligibility proofs to the network
		// - relay the eligibility proof to the consensus process
		// - return error so the node don't relay messages from malicious parties
		if err := b.handleMaliciousHareMessage(ctx, logger, nodeID, proof, iMsg, out); err != nil {
			return err
		}
		return fmt.Errorf("known malicious %v", nodeID.String())
	}

	logger.With().Debug("broker forwarding message to outbox",
		log.Int("queue_size", len(out)))
	select {
	case out <- iMsg:
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return errClosed
	}
	return nil
}

func (b *Broker) handleMaliciousHareMessage(
	ctx context.Context,
	logger log.Log,
	nodeID types.NodeID,
	proof *types.MalfeasanceProof,
	msg *Msg,
	out chan any,
) error {
	gossip := &types.MalfeasanceGossip{
		MalfeasanceProof: *proof,
		Eligibility: &types.HareEligibilityGossip{
			Layer:       msg.Layer,
			Round:       msg.Round,
			PubKey:      msg.PubKey.Bytes(),
			Eligibility: msg.Eligibility,
		},
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return errClosed
	case b.mchOut <- gossip:
		// causes the node to gossip the malfeasance and eligibility proof
	}

	b.Log.WithContext(ctx).With().Debug("broker forwarding hare eligibility to consensus process",
		log.Int("queue_size", len(out)))
	select {
	case out <- gossip.Eligibility:
	case <-ctx.Done():
	case <-b.ctx.Done():
	}
	return nil
}

func (b *Broker) HandleEligibility(ctx context.Context, em *types.HareEligibilityGossip) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if em == nil {
		b.Log.WithContext(ctx).Fatal("invalid hare eligibility")
	}
	lid := em.Layer
	out, exist := b.outbox[lid.Uint32()]
	if !exist {
		b.Log.WithContext(ctx).With().Debug("consensus not running for layer", lid)
		return false
	}

	nodeID := types.BytesToNodeID(em.PubKey)
	isActive, err := b.stateQuerier.IsIdentityActiveOnConsensusView(ctx, nodeID, em.Layer)
	if err != nil {
		b.Log.WithContext(ctx).With().Error("failed to check if identity is active",
			em.Layer,
			log.Uint32("round", em.Round),
			log.Stringer("smesher", nodeID),
			log.Err(err))
		return false
	}
	if !isActive {
		b.Log.WithContext(ctx).With().Debug("identity is not active",
			em.Layer,
			log.Uint32("round", em.Round),
			log.Stringer("smesher", nodeID))
		return false
	}
	if !b.roleValidator.ValidateEligibilityGossip(ctx, em) {
		b.Log.WithContext(ctx).With().Debug("invalid gossip eligibility",
			em.Layer,
			log.Uint32("round", em.Round),
			log.Stringer("smesher", nodeID))
		return false
	}
	b.Log.WithContext(ctx).With().Debug("broker forwarding gossip eligibility to consensus process",
		log.Int("queue_size", len(out)))
	select {
	case out <- em:
		return true
	case <-ctx.Done():
	case <-b.ctx.Done():
	}
	return false
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages.
func (b *Broker) Register(ctx context.Context, id types.LayerID) (chan any,
	error,
) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !id.After(b.latestLayer) { // should expect to update only newer layers, its a coding error if this happens
		panic(fmt.Sprintf("broker register called with old layer, current layer: %v, given layer: %v", b.latestLayer, id))
	}

	// We check here since early messages could have already constructed this map entry
	outboxCh, exist := b.outbox[id.Uint32()]
	if !exist {
		outboxCh = make(chan any, inboxCapacity)
		b.outbox[id.Uint32()] = outboxCh
	}
	b.latestLayer = id

	return outboxCh, nil
}

// Unregister a layer from receiving messages.
func (b *Broker) Unregister(ctx context.Context, id types.LayerID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.outbox, id.Uint32())
	b.WithContext(ctx).With().Debug("hare broker unregistered layer", id)
}

// Close closes broker.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancel()
}
