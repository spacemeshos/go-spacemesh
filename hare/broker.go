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
	pending         map[types.LayerID][]any // the buffer of pending early messages for the next layer
	latestLayer     types.LayerID           // the latest layer to attempt register (successfully or unsuccessfully)
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
		pending:         make(map[types.LayerID][]any),
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
	errUnregistered      = errors.New("layer is unregistered")
	errNotSynced         = errors.New("layer is not synced")
	errFutureMsg         = errors.New("future message")
	errRegistration      = errors.New("failed during registration")
	errInstanceNotSynced = errors.New("instance not synchronized")
	errClosed            = errors.New("closed")
)

func (b *Broker) validateTiming(ctx context.Context, m *Message) error {
	msgInstID := m.Layer
	if b.getInbox(msgInstID) != nil {
		b.Log.WithContext(ctx).With().Debug("instance exists", msgInstID)
		return nil
	}

	latestLayer := b.getLatestLayer()

	if msgInstID.Before(latestLayer) {
		return fmt.Errorf("%w: msg %v, latest %v", errUnregistered, msgInstID.Uint32(), latestLayer)
	}

	// current layer
	if msgInstID == latestLayer {
		return fmt.Errorf("%w: latest %v", errRegistration, latestLayer)
	}

	// early msg
	if msgInstID == latestLayer.Add(1) {
		return fmt.Errorf("%w: latest %v", errEarlyMsg, latestLayer)
	}

	// future msg
	return fmt.Errorf("%w: msg %v, latest %v", errFutureMsg, msgInstID, latestLayer)
}

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
	logger := b.WithContext(ctx).WithFields(log.Stringer("latest_layer", b.getLatestLayer()), h)
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

	isEarly := false
	if err = b.validateTiming(ctx, &hareMsg); err != nil {
		if !errors.Is(err, errEarlyMsg) {
			// not early, validation failed
			logger.With().Debug("broker received a message to a consensus process that is not registered",
				log.Err(err))
			return err
		}

		logger.With().Debug("early message detected", log.Err(err))
		isEarly = true
	}

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
		if err := b.handleMaliciousHareMessage(ctx, logger, iMsg.NodeID, proof, iMsg, isEarly); err != nil {
			return err
		}
		return fmt.Errorf("known malicious %v", iMsg.NodeID.String())
	}

	if isEarly {
		return b.handleEarlyMessage(logger, msgLayer, iMsg.NodeID, iMsg)
	}

	// has instance, just send
	out := b.getInbox(msgLayer)
	if out == nil {
		logger.With().Debug("missing broker instance for layer", msgLayer)
		return errTooOld
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
	early bool,
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

	toRelay := gossip.Eligibility
	if early {
		return b.handleEarlyMessage(logger, msg.Layer, nodeID, toRelay)
	}

	out := b.getInbox(msg.Layer)
	if out == nil {
		b.Log.WithContext(ctx).With().Debug("consensus not running for layer", msg.Layer)
		return nil
	}
	b.Log.WithContext(ctx).With().Debug("broker forwarding hare eligibility to consensus process",
		log.Int("queue_size", len(out)))
	select {
	case out <- toRelay:
	case <-ctx.Done():
	case <-b.ctx.Done():
	}
	return nil
}

func (b *Broker) HandleEligibility(ctx context.Context, em *types.HareEligibilityGossip) bool {
	if em == nil {
		b.Log.WithContext(ctx).Fatal("invalid hare eligibility")
	}
	lid := em.Layer
	out := b.getInbox(lid)
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

func (b *Broker) getInbox(id types.LayerID) chan any {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out, exist := b.outbox[id]
	if !exist {
		return nil
	}
	return out
}

func (b *Broker) handleEarlyMessage(logger log.Log, layer types.LayerID, nodeID types.NodeID, msg any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exist := b.pending[layer]; !exist { // create buffer if first msg
		b.pending[layer] = make([]any, 0, inboxCapacity)
	}

	// we want to write all buffered messages to a chan with InboxCapacity len
	// hence, we limit the buffer for pending messages
	if len(b.pending[layer]) == inboxCapacity {
		logger.With().Warning("too many pending messages, ignoring message",
			log.Int("inbox_capacity", inboxCapacity),
			log.Stringer("smesher", nodeID))
		return nil
	}
	b.pending[layer] = append(b.pending[layer], msg)
	return nil
}

func (b *Broker) cleanOldLayers() {
	b.mu.Lock()
	defer b.mu.Unlock()

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
func (b *Broker) Register(ctx context.Context, id types.LayerID) (chan any, error) {
	b.setLatestLayer(ctx, id)

	// check to see if the node is still synced
	if !b.Synced(ctx, id) {
		return nil, errInstanceNotSynced
	}
	return b.createNewInbox(id), nil
}

func (b *Broker) createNewInbox(id types.LayerID) chan any {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.outbox) >= b.limit {
		// unregister the earliest layer to make space for the new layer
		// cannot call unregister here because unregister blocks and this would cause a deadlock
		instance := b.minDeleted.Add(1)
		delete(b.outbox, instance)
		b.minDeleted = instance
		b.With().Info("unregistered layer due to maximum concurrent processes", instance)
	}
	outboxCh := make(chan any, inboxCapacity)
	b.outbox[id] = outboxCh
	for _, mOut := range b.pending[id] {
		outboxCh <- mOut
	}
	delete(b.pending, id)
	return outboxCh
}

func (b *Broker) cleanupInstance(id types.LayerID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.outbox, id)
}

// Unregister a layer from receiving messages.
func (b *Broker) Unregister(ctx context.Context, id types.LayerID) {
	b.cleanupInstance(id)
	b.cleanOldLayers()
	b.WithContext(ctx).With().Debug("hare broker unregistered layer", id)
}

// Synced returns true if the given layer is synced, false otherwise.
func (b *Broker) Synced(ctx context.Context, id types.LayerID) bool {
	return b.nodeSyncState.IsSynced(ctx) && b.nodeSyncState.IsBeaconSynced(id.GetEpoch())
}

// Close closes broker.
func (b *Broker) Close() {
	b.cancel()
}

func (b *Broker) getLatestLayer() types.LayerID {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.latestLayer
}

func (b *Broker) setLatestLayer(ctx context.Context, layer types.LayerID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !layer.After(b.latestLayer) { // should expect to update only newer layers
		b.WithContext(ctx).With().Error("tried to update a previous layer",
			log.Stringer("this_layer", layer),
			log.Stringer("prev_layer", b.latestLayer))
		return
	}

	b.latestLayer = layer
}
