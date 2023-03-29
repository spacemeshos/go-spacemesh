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
	roleValidator   validator                // provides eligibility validation
	stateQuerier    stateQuerier             // provides activeness check
	nodeSyncState   system.SyncStateProvider // provider function to check if the node is currently synced
	mchOut          chan<- *types.MalfeasanceGossip
	outbox          map[types.LayerID]chan any
	latestLayer     types.LayerID // the latest layer to attempt register (successfully or unsuccessfully)
	minDeleted      types.LayerID
	limit           int // max number of simultaneous consensus processes

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
		outbox:          make(map[types.LayerID]chan any),
		latestLayer:     types.GetEffectiveGenesis(),
		limit:           limit,
		minDeleted:      types.GetEffectiveGenesis(),
	}
	b.ctx, b.cancel = context.WithCancel(context.Background())
	return b
}

// Start listening to Hare messages (non-blocking).
func (b *Broker) Start(ctx context.Context) {
	b.once.Do(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.cancel != nil {
			b.cancel()
		}
		b.ctx, b.cancel = context.WithCancel(ctx)
	})
}

var (
	errNotSynced         = errors.New("layer is not synced")
	errInstanceNotSynced = errors.New("instance not synchronized")
	errClosed            = errors.New("closed")
)

// HandleMessage separate listener routine that receives gossip messages and adds them to the priority queue.
func (b *Broker) HandleMessage(ctx context.Context, _ p2p.Peer, msg []byte) pubsub.ValidationResult {
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

	// lock over b.latestLayer access
	b.mu.Lock()
	logger := b.WithContext(ctx).WithFields(log.Stringer("latest_layer", b.latestLayer), h)
	b.mu.Unlock()

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
	if !b.Synced(ctx, msgLayer) {
		return errNotSynced
	}

	var out chan any
	err = func() error {
		b.mu.Lock()
		defer b.mu.Unlock()
		var exist bool
		out, exist = b.outbox[msgLayer]
		if !exist {
			// If the message is not early then return
			if msgLayer != b.latestLayer.Add(1) {
				logger.With().Debug(
					"broker received a message to a consensus process that is not registered",
					msgLayer.Field(),
				)
				return fmt.Errorf(
					"ignoring message to unregistered consensus process, latestLayer: %v, messageLayer: %v",
					b.latestLayer,
					msgLayer.Uint32(),
				)
			}
			// message is early
			logger.With().Debug(
				"early message detected",
				msgLayer.Field(),
			)
			out = make(chan any, inboxCapacity)
			b.outbox[msgLayer] = out
		}
		return nil
	}()
	if err != nil {
		return err
	}

	b.Log.WithContext(ctx).With().Debug("instance exists", msgLayer)

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

	if proof, err := b.msh.GetMalfeasanceProof(iMsg.NodeID); err != nil && !errors.Is(err, sql.ErrNotFound) {
		logger.With().Error("failed to check malicious identity",
			log.Stringer("smesher", iMsg.NodeID),
			log.Err(err),
		)
		return err
	} else if proof != nil {
		// when hare receives a hare gossip from a malicious identity,
		// - gossip its malfeasance + eligibility proofs to the network
		// - relay the eligibility proof to the consensus process
		// - return error so the node don't relay messages from malicious parties
		if err := b.handleMaliciousHareMessage(ctx, logger, iMsg.NodeID, proof, iMsg, out); err != nil {
			return err
		}
		return fmt.Errorf("known malicious %v", iMsg.NodeID.String())
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
			NodeID:      msg.NodeID,
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
	out := b.outbox[lid]
	if out == nil {
		b.Log.WithContext(ctx).With().Debug("consensus not running for layer", lid)
		return false
	}

	isActive, err := b.stateQuerier.IsIdentityActiveOnConsensusView(ctx, em.NodeID, em.Layer)
	if err != nil {
		b.Log.WithContext(ctx).With().Error("failed to check if identity is active",
			em.Layer,
			log.Uint32("round", em.Round),
			log.Stringer("smesher", em.NodeID),
			log.Err(err),
		)
		return false
	}
	if !isActive {
		b.Log.WithContext(ctx).With().Debug("identity is not active",
			em.Layer,
			log.Uint32("round", em.Round),
			log.Stringer("smesher", em.NodeID),
		)
		return false
	}
	if !b.roleValidator.ValidateEligibilityGossip(ctx, em) {
		b.Log.WithContext(ctx).With().Debug("invalid gossip eligibility",
			em.Layer,
			log.Uint32("round", em.Round),
			log.Stringer("smesher", em.NodeID),
		)
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

func (b *Broker) cleanOldLayers() {
	for i := b.minDeleted.Add(1); i.Before(b.latestLayer); i = i.Add(1) {
		_, exist := b.outbox[i]

		if !exist { // unregistered
			b.minDeleted = b.minDeleted.Add(1)
		} else { // encountered first still running layer
			break
		}
	}
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages.
func (b *Broker) Register(ctx context.Context, id types.LayerID) (chan any,
	error,
) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if id.After(b.latestLayer) { // should expect to update only newer layers
		b.latestLayer = id
	} else {
		b.WithContext(ctx).With().Error("tried to update a previous layer",
			log.Stringer("this_layer", id),
			log.Stringer("prev_layer", b.latestLayer))
	}

	// check to see if the node is still synced
	if !b.Synced(ctx, id) {
		return nil, errInstanceNotSynced
	}
	// Delete old outbox beyond the limit
	if len(b.outbox) >= b.limit {
		// unregister the earliest layer to make space for the new layer
		// cannot call unregister here because unregister blocks and this would cause a deadlock
		instance := b.minDeleted.Add(1)
		delete(b.outbox, instance)
		b.minDeleted = instance
		b.With().Info("unregistered layer due to maximum concurrent processes", instance)
	}
	// Check for existing outbox, this can happen if we receive early messages
	// for the next layer.
	outboxCh, exist := b.outbox[id]
	if !exist {
		// Create it if it does not exist
		outboxCh = make(chan any, inboxCapacity)
		b.outbox[id] = outboxCh
	}
	return outboxCh, nil
}

// Unregister a layer from receiving messages.
func (b *Broker) Unregister(ctx context.Context, id types.LayerID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.outbox, id)
	b.cleanOldLayers()
	b.WithContext(ctx).With().Debug("hare broker unregistered layer", id)
}

// Synced returns true if the given layer is synced, false otherwise.
func (b *Broker) Synced(ctx context.Context, id types.LayerID) bool {
	return b.nodeSyncState.IsSynced(ctx) && b.nodeSyncState.IsBeaconSynced(id.GetEpoch())
}

// Close closes broker.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancel()
}
