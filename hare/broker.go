package hare

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/system"
)

type validator interface {
	Validate(context.Context, *Message) bool
	ValidateEligibilityGossip(context.Context, *types.HareEligibilityGossip) bool
}

type messageStore struct {
	messages map[types.Hash20]*Message
}

func newMessageStore() *messageStore {
	return &messageStore{
		messages: make(map[types.Hash20]*Message),
	}
}

func (h *messageStore) storeMessage(hash types.Hash20, m *Message) {
	h.messages[hash] = m
}

func (h *messageStore) buildMalfeasanceProof(a, b types.Hash20) *types.MalfeasanceProof {
	proofMsg := func(hash types.Hash20) types.HareProofMsg {
		msg := h.messages[a]

		return types.HareProofMsg{
			InnerMsg: types.HareMetadata{
				Layer:   msg.Layer,
				Round:   msg.Round,
				MsgHash: types.BytesToHash(msg.HashBytes()),
			},
			SmesherID: msg.SmesherID,
			Signature: msg.Signature,
		}
	}

	return &types.MalfeasanceProof{
		Layer: h.messages[a].Layer,
		Proof: types.Proof{
			Type: types.HareEquivocation,
			Data: &types.HareProof{
				Messages: [2]types.HareProofMsg{proofMsg(a), proofMsg(b)},
			},
		},
	}
}

// Broker is the dispatcher of incoming Hare messages.
// The broker validates that the sender is eligible and active and forwards the message to the corresponding outbox.
type Broker struct {
	log.Log
	mu sync.RWMutex

	msh           mesh
	edVerifier    *signing.EdVerifier
	roleValidator validator                // provides eligibility validation
	stateQuerier  stateQuerier             // provides activeness check
	nodeSyncState system.SyncStateProvider // provider function to check if the node is currently synced
	latestLayer   types.LayerID            // the latest layer to attempt register (successfully or unsuccessfully)

	// So put messages in messages, including early messages, when we start we
	// iterate the messages and push them into the handler and when we need
	// messages for constructing a malfeasance proof we have them right there.
	messages map[types.LayerID]*messageStore
	handlers map[types.LayerID]*hare3.Handler

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func newBroker(
	msh mesh,
	edVerifier *signing.EdVerifier,
	roleValidator validator,
	stateQuerier stateQuerier,
	syncState system.SyncStateProvider,
	limit int,
	log log.Log,
) *Broker {
	b := &Broker{
		Log:           log,
		msh:           msh,
		edVerifier:    edVerifier,
		roleValidator: roleValidator,
		stateQuerier:  stateQuerier,
		nodeSyncState: syncState,
		messages:      make(map[types.LayerID]*messageStore),
		latestLayer:   types.GetEffectiveGenesis(),
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

func parts(msg *Message) (id []byte, round int8, values []types.Hash20) {
	values = make([]types.Hash20, len(msg.Values))
	for i := range msg.Values {
		values[i] = types.Hash20(msg.Values[i])
	}
	return msg.SmesherID[:], int8(msg.Round), values
}

// HandleMessage receives messages from the network and forwards them to the
// hare handler for a specific layer. The returned error controls whether a
// received message will be relayed to the network, a nil error indicates that
// the received message should be relayed to the network, a non nil error
// indicates that the message should not be relayed (the message will be
// dropped).
//
// If no layer is registered and the message is not for the next layer then the
// message is dropped. Messages also undergo validation, if that is failed then
// the message is also dropped. Additionally the hare handler will also drop
// any messages for a given round where it has already seen at least one
// eqivocating message for that round.
//
// An equivocating message is considered to be either a message from a
// pre-known malfeasant identity (one for which we had a malfeasacne proof) or
// any messages beyond the first for a given identity in a given round.
func (b *Broker) HandleMessage(ctx context.Context, _ p2p.Peer, msg []byte) error {
	select {
	case <-ctx.Done():
		return errClosed
	case <-b.ctx.Done():
		return errClosed
	default:
	}

	latestLayer := b.getLatestLayer()
	hash := types.CalcMessageHash20(msg, pubsub.HareProtocol)
	logger := b.WithContext(ctx).WithFields(log.Stringer("latest_layer", b.getLatestLayer()), hash.ToHash12())
	hareMsg, err := MessageFromBuffer(msg)
	if err != nil {
		logger.With().Error("failed to build message", hash.ToHash12(), log.Err(err))
		b.WithContext(ctx).With().Error("failed to build message", hash.ToHash12(), log.Err(err))
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

	b.mu.Lock()
	defer b.mu.Unlock()
	msgs := b.messages[hareMsg.Layer]

	early := hareMsg.Layer == latestLayer+1
	// Do a quick exit if this message is not early and not for a registered layer
	if msgs == nil && !early {
		return errors.New("consensus process not registered")
	}

	if !b.edVerifier.Verify(signing.HARE, hareMsg.SmesherID, hareMsg.SignedBytes(), hareMsg.Signature) {
		logger.With().Error("failed to verify signature",
			log.Int("sig_len", len(hareMsg.Signature)),
		)
		return fmt.Errorf("verify ed25519 signature")
	}
	// create msg
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

	id, round, values := parts(hareMsg)
	proof, err := b.msh.GetMalfeasanceProof(hareMsg.SmesherID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		logger.With().Error("failed to check malicious identity", log.Stringer("smesher", hareMsg.SmesherID), log.Err(err))
		return err
	}

	if proof != nil {
		// The way we signify a message from a know malfeasant identity to the
		// protocol is a message without values.
		values = nil
	}

	// If the message is early we may need to initialize the message store and handler.
	if early {
		if msgs == nil {
			msgs = newMessageStore()
			b.messages[hareMsg.Layer] = msgs
			// TODO actually build the handler
			b.handlers[hareMsg.Layer] = hare3.NewHandler(nil, nil, nil, nil)
		}
	}
	shouldRelay, equivocationHash := b.handlers[hareMsg.Layer].HandleMsg(hash, id, round, values)
	// If we detect a new equivocation then store it.
	if equivocationHash != nil {
		proof := msgs.buildMalfeasanceProof(hash, *equivocationHash)
		err := b.msh.AddMalfeasanceProof(hareMsg.SmesherID, proof, nil)
		if err != nil {
			logger.With().Error("faild to add newly discovered malfeasance proof to mesh", log.Err(err))
		}
	}
	// If this message shouldn't be relayed return an error to indicate this to the p2p system.
	if !shouldRelay {
		return errors.New("don't relay")
	}
	// Only store messages the get relayed
	msgs.storeMessage(hash, hareMsg)

	// Returning nil lets the p2p gossipsub layer know that the received
	// message was valid and it should relay it to the network.
	return nil
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages.
func (b *Broker) Register(ctx context.Context, id types.LayerID) (*hare3.Handler, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// check to see if the node is still synced
	if !b.Synced(ctx, id) {
		return nil, errInstanceNotSynced
	}

	if !id.After(b.latestLayer) { // should expect to update only newer layer
		return nil, fmt.Errorf("tried to update to a previous layer, broker at: %v, given layer %v", b.latestLayer, id)
	}
	b.latestLayer = id
	msgs := b.messages[id]
	if msgs == nil {
		msgs = newMessageStore()
		b.messages[id] = msgs
		b.handlers[id] = hare3.NewHandler(nil, nil, nil, nil)
	}

	return b.handlers[id], nil
}

// Unregister a layer from receiving messages.
func (b *Broker) Unregister(ctx context.Context, id types.LayerID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.messages, id)
	delete(b.handlers, id)
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
