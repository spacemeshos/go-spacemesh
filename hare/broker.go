package hare

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/system"
)

const inboxCapacity = 1024 // inbox size per instance

type validator interface {
	Validate(context.Context, *Message) bool
	ValidateEligibilityGossip(context.Context, *types.HareEligibilityGossip) bool
}

// Broker is the dispatcher of incoming Hare messages.
// The broker validates that the sender is eligible and active and forwards the message to the corresponding outbox.
type Broker struct {
	log.Log
	mu sync.RWMutex

	cfg           config.Config
	msh           mesh
	edVerifier    *signing.EdVerifier
	roleValidator validator                // provides eligibility validation
	stateQuerier  stateQuerier             // provides activeness check
	nodeSyncState system.SyncStateProvider // provider function to check if the node is currently synced
	publisher     pubsub.Publisher
	outbox        map[types.LayerID]chan any
	trackers      map[types.LayerID]*EligibilityTracker
	pending       map[types.LayerID][]any // the buffer of pending early messages for the next layer
	latestLayer   types.LayerID           // the latest layer to attempt register (successfully or unsuccessfully)
	minDeleted    types.LayerID
	limit         int // max number of simultaneous consensus processes

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func newBroker(
	cfg config.Config,
	msh mesh,
	edVerifier *signing.EdVerifier,
	roleValidator validator,
	stateQuerier stateQuerier,
	syncState system.SyncStateProvider,
	publisher pubsub.Publisher,
	limit int,
	log log.Log,
) *Broker {
	b := &Broker{
		Log:           log,
		cfg:           cfg,
		msh:           msh,
		edVerifier:    edVerifier,
		roleValidator: roleValidator,
		stateQuerier:  stateQuerier,
		nodeSyncState: syncState,
		publisher:     publisher,
		trackers:      map[types.LayerID]*EligibilityTracker{},
		outbox:        make(map[types.LayerID]chan any),
		pending:       make(map[types.LayerID][]any),
		latestLayer:   types.GetEffectiveGenesis(),
		limit:         limit,
		minDeleted:    types.GetEffectiveGenesis(),
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
func (b *Broker) HandleMessage(ctx context.Context, _ p2p.Peer, msg []byte) error {
	select {
	case <-ctx.Done():
		return errClosed
	case <-b.ctx.Done():
		return errClosed
	default:
	}

	h := types.CalcMessageHash12(msg, pubsub.HareProtocol)
	logger := b.WithContext(ctx).WithFields(log.Stringer("latest_layer", b.getLatestLayer()), h)
	hareMsg, err := MessageFromBuffer(msg)
	if err != nil {
		logger.With().Error("failed to build message", h, log.Err(err))
		return err
	}
	logger = logger.WithFields(log.Inline(hareMsg))

	if hareMsg.InnerMessage == nil {
		logger.With().Warning("hare msg missing inner msg", log.Err(errNilInner))
		return errNilInner
	}

	logger.Debug("broker received hare message")

	msgLayer := hareMsg.Layer
	if !b.Synced(ctx, msgLayer) {
		return errNotSynced
	}

	isEarly := false
	if err := b.validateTiming(ctx, hareMsg); err != nil {
		if !errors.Is(err, errEarlyMsg) {
			// not early, validation failed
			logger.With().Debug("broker received a message to a consensus process that is not registered",
				log.Err(err))
			return err
		}

		logger.With().Debug("early message detected", log.Err(err))
		isEarly = true
	}

	if !b.edVerifier.Verify(signing.HARE, hareMsg.SmesherID, hareMsg.SignedBytes(), hareMsg.Signature) {
		logger.With().Error("failed to verify signature",
			log.Int("sig_len", len(hareMsg.Signature)),
		)
		return fmt.Errorf("verify ed25519 signature")
	}
	hareMsg.signedHash = types.BytesToHash(hareMsg.InnerMessage.HashBytes())

	if err := checkIdentity(ctx, b.Log, hareMsg, b.stateQuerier); err != nil {
		logger.With().Warning("message validation failed: could not construct msg", log.Err(err))
		return err
	}

	// validate msg
	if !b.roleValidator.Validate(ctx, hareMsg) {
		logger.Warning("message validation failed: eligibility validator returned false")
		return errors.New("not eligible")
	}

	// validation passed, report
	logger.With().Debug("broker reported hare message as valid")

	if proof, err := b.msh.GetMalfeasanceProof(hareMsg.SmesherID); err != nil && !errors.Is(err, sql.ErrNotFound) {
		logger.With().Error("failed to check malicious identity",
			log.Stringer("smesher", hareMsg.SmesherID),
			log.Err(err),
		)
		return err
	} else if proof != nil {
		// when hare receives a hare gossip from a malicious identity,
		// - gossip its malfeasance + eligibility proofs to the network
		// - relay the eligibility proof to the consensus process
		// - return error so the node don't relay messages from malicious parties
		b.handleMaliciousHareMessage(ctx, hareMsg.SmesherID, proof, hareMsg)
		return fmt.Errorf("known malicious %v", hareMsg.SmesherID.String())
	}

	if isEarly {
		return b.handleEarlyMessage(logger, msgLayer, hareMsg.SmesherID, hareMsg)
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
	case out <- hareMsg:
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return errClosed
	}
	return nil
}

func (b *Broker) handleMaliciousHareMessage(
	ctx context.Context,
	nodeID types.NodeID,
	proof *types.MalfeasanceProof,
	msg *Message,
) {
	// do not re-gossip known eligibility proof
	reGossip := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.trackers[msg.Layer]; !ok {
			b.trackers[msg.Layer] = NewEligibilityTracker(b.cfg.N)
		}
		return !b.trackers[msg.Layer].Track(nodeID, msg.Round, msg.Eligibility.Count, false)
	}()
	if !reGossip {
		return
	}

	gossip := &types.MalfeasanceGossip{
		MalfeasanceProof: *proof,
		Eligibility: &types.HareEligibilityGossip{
			Layer:       msg.Layer,
			Round:       msg.Round,
			NodeID:      msg.SmesherID,
			Eligibility: msg.Eligibility,
		},
	}

	gossipBytes, err := codec.Encode(gossip)
	if err != nil {
		b.Log.With().Fatal("failed to encode MalfeasanceGossip (broker)",
			log.Context(ctx),
			gossip.Eligibility.NodeID,
			log.Err(err),
		)
	}
	if err = b.publisher.Publish(ctx, pubsub.MalfeasanceProof, gossipBytes); err != nil {
		b.Log.With().Error("failed to gossip MalfeasanceProof (broker)",
			log.Context(ctx),
			gossip.Eligibility.NodeID,
			log.Err(err),
		)
	}
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

func (b *Broker) CleanOldLayers(current types.LayerID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := b.minDeleted + 1; i <= current; i++ {
		if _, exist := b.outbox[i]; !exist { // unregistered
			b.minDeleted = i
		} else { // encountered first still running layer
			break
		}
	}
	for lid := range b.trackers {
		if lid <= b.minDeleted {
			delete(b.trackers, lid)
			delete(b.pending, lid)
		}
	}
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages.
func (b *Broker) Register(ctx context.Context, id types.LayerID) (chan any, *EligibilityTracker, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !id.After(b.latestLayer) { // should expect to update only newer layers
		b.WithContext(ctx).With().Error("tried to update a previous layer",
			log.Stringer("this_layer", id),
			log.Stringer("prev_layer", b.latestLayer))
	}

	b.latestLayer = id

	// check to see if the node is still synced
	if !b.Synced(ctx, id) {
		return nil, nil, errInstanceNotSynced
	}

	if len(b.outbox) >= b.limit {
		// unregister the earliest layer to make space for the new layer
		// cannot call unregister here because unregister blocks and this would cause a deadlock
		min := types.LayerID(0)
		for lid := range b.outbox {
			min = types.MinLayer(min, lid)
		}
		delete(b.outbox, min)
		b.minDeleted = min
		b.With().Info("unregistered layer due to maximum concurrent processes", min)
	}
	outboxCh := make(chan any, inboxCapacity)
	b.outbox[id] = outboxCh
	for _, mOut := range b.pending[id] {
		outboxCh <- mOut
	}
	delete(b.pending, id)
	if _, ok := b.trackers[id]; !ok {
		b.trackers[id] = NewEligibilityTracker(b.cfg.N)
	}
	return outboxCh, b.trackers[id], nil
}

// Unregister a layer from receiving messages.
func (b *Broker) Unregister(ctx context.Context, id types.LayerID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.outbox, id)
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
