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

type handler struct {
	*hare3.Handler
	messages map[types.Hash20]*Message
	mu       sync.Mutex
}

func newHandler() *handler {
	return &handler{
		messages: make(map[types.Hash20]*Message),
	}
}

func (h *handler) storeMessage(hash types.Hash20, m *Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages[hash] = m
}

func (h *handler) retreiveMessage(hash types.Hash20) *Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.messages[hash]
}

func (h *handler) processEarlyMessages() []*types.MalfeasanceProof {
	// TODO I realised that this is not sufficient, we need to send these
	// messages through the real handler. It would be nice not to decode them
	// again, so I should probably extract out a section without decoding.
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, v := range h.messages {

		id, round, vals := parts(v)
		_, equivocation := h.Handler.HandleMsg(k, id, round, vals)
		if equivocation != nil {
			equivocations = append(equivocations, []types.Hash20{k, *equivocation})
		}
		// TODO do equivocation sending, send on mch
	}
	return equivocations
}

func (h *handler) buildMalfeasanceProof(a, b types.Hash20) *types.MalfeasanceProof {
	h.mu.Lock()
	defer h.mu.Unlock()

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
	handlers map[types.LayerID]*handler

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
		handlers:      make(map[types.LayerID]*handler),
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
	errUnregistered      = errors.New("layer is unregistered")
	errNotSynced         = errors.New("layer is not synced")
	errFutureMsg         = errors.New("future message")
	errRegistration      = errors.New("failed during registration")
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

// HandleMessage separate listener routine that receives gossip messages and adds them to the priority queue.
func (b *Broker) HandleMessage(ctx context.Context, _ p2p.Peer, msg []byte) error {
	select {
	case <-ctx.Done():
		return errClosed
	case <-b.ctx.Done():
		return errClosed
	default:
	}

	hash := types.CalcMessageHash20(msg, pubsub.HareProtocol)
	hareMsg, err := MessageFromBuffer(msg)
	if err != nil {
		b.WithContext(ctx).With().Error("failed to build message", hash.ToHash12(), log.Err(err))
		return err
	}
	return b.HandleDecodedMessage(ctx, hash, hareMsg)
}

func (b *Broker) HandleDecodedMessage(ctx context.Context, msgHash types.Hash20, hareMsg *Message) error {
	latestLayer := b.getLatestLayer()
	logger := b.WithContext(ctx).WithFields(log.Stringer("latest_layer", latestLayer), msgHash.ToHash12())
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

	// handler, msgEarly := b.getHandler(msgHash, hareMsg, latestLayer)
	// if handler == nil {
	// 	if msgEarly {
	// 		logger.With().Debug("early message detected", msgLayer.Field())
	// 		return nil
	// 	}
	// 	logger.With().Debug("broker received a message to a consensus process that is not registered",
	// 		msgLayer.Field())
	// 	return errors.New("consensus process not registered")
	// }

	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.handlers[hareMsg.Layer]

	// nil handler - is early
	// nil handler - is not early - reject
	// if its early then the handler could be nil, otherwise handler is non nil.

	early := hareMsg.Layer = latestLayer+1
	// Do a quick exit if this message is not early and not for a registered layer
	if h == nil && !early {
		return errors.New("consensus process not registered")
	}
	// NOTE We need to process the rest of the validation here in the case that
	// we have an early message to understand if we forward it. We need to take
	// into account the malfeasacne proofs at this point as well so that nodes
	// can't spam early messages. This means we need to extend the message
	// store to keep track of already detected malfeasance proofs. So we need
	// to look up whether we had a proof for a given round and id. So
	// map[id]map[round]bool, this should be done after the not registerd
	// process check. We still have to forward messages that we had a prior
	// malfeasacne proof for that identity because they are still contributing
	// to the network numbers.

	// // If the handler has already been registered then return it.
	// if h != nil && h.Handler != nil {
	// 	h.storeMessage(hash, msg)
	// 	return h, false
	// }

	// // If the message is early we want to store it so that it can be processed
	// // when a handler is registerd for this layer. Early messagges must be
	// // stored before b.mu.Unlock, to ensure that calls to Register see all
	// // early messages.
	// if msg.Layer == latestLayer+1 {
	// 	// Build the handler if we didn't already
	// 	if h == nil {
	// 		h = newHandler()
	// 		b.handlers[msg.Layer] = h
	// 	}
	// 	h.storeMessage(hash, msg)
	// 	return nil, true
	// }

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

	proof, err := b.msh.GetMalfeasanceProof(hareMsg.SmesherID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		logger.With().Error("failed to check malicious identity", log.Stringer("smesher", hareMsg.SmesherID), log.Err(err))
		return err
	}
	if proof != nil {
		if handler.hasProof(hareMsg.Round, hareMsg.SmesherID) {
			// We already handled a message from this malfeasant identity this round, so we drop this one.
			logger.With().Debug("message validation failed: equivocating message from prior known malfeasant identity", log.Stringer("smesher", hareMsg.SmesherID))
			return errors.New("equivocating message from prior known malfeasant identity")
		}
		// The way we signify a malfeasance proof to the protocol is a message without values.
		values = nil
	}

	id, round, values := parts(hareMsg)

	// How do we want to handle the known maliciouos identities? Do we need to
	// gossip, well if a node dropped out of a layer or two then it wont know
	// and untill we have state based syncing we can't guarantee that nodes
	// will have prior offenders in their mesh. So safer to send I think, and
	// of course we still need to store. But only this needs to be sent we
	// don't need to send proofs for equivocations discovered in this round,
	// because everyone will have them. However this means that we do need to
	// react to a malfeasance gossip. So given that we should probably gossip
	// the malfeasance instead of the message in the case of freshly discovered
	// malfeasance.
	//
	// OK so I've decided that we won't gossip malfeasance proofs for now. It's
	// just overcomplicating things, and it's unclear what kind of performance
	// enhancement it would provide.
	//
	// OK no this is not going to work because equivocation detection happens
	// inside the protocol implementation. And so unless we call the protocol,
	// we can't detect equivocation and so we can't know whether we need to
	// relay a message or not without calling the protocol. So we need to have
	// a protocol factory in the broker.
	//
	// NOTE But we can't do that, because we need to know the set of proposals we have in order to process messages, do we though? Actually maybe that is not needed for the handler!!! Yeah it's not.
	// We only need the values for the protocol when we actually run it, we can build the handler earlier, and actually have that fed back to hare to build the protocol.
	// 
	// So yes a protocol factory would work in the broker, really we just need the handler part.
	//
	//---------------------------------------------------- So this is it!
	// We fully process early messages through the handler and we will know if we need to relay them at that point. At some point later the protocol will be run with the contents of the handler.
	// 
	// lets build a table of what forwarding action we take for each situation.
	//
	// 1. A good message with no equivoction - Relay
	// 2. A messge where eqivocation is discovered - Relay
	// 3. A message where equivocation was previously discovered - Don't relay
	// 4. A message for which there was a prior malfeasance proof and for which there was not a previously discoverd eqivocation - relay, needs to be relayed because, nodes need to count this participant, even if they don't consider the message valid.
	// 5. A message for which there was a prior malfeasance proof and for which there was a previously discoverd equivoction - don't relay because of the prior discovered equivocation.
	// The second equivocating message for which there was a prior malfeasance proof for both messages - don't relay, we relayed the first one.
	//
	// Now lets think about those cases in the context of early messages.
	// Case 1 we can't detect for early messages without running the whole protocol, which we can't because we won't have the set of proposals until it's time to start.
	// Case 2 again we can't discover equivocation without running the whole protocol.
	// Case 3 again we can't know because we won't have discoved the equivocation.
	// Case 4 we can't know about previous equivocations
	// Case 5 again we can't know.
	//
	// I see 3 approaches to solving this
	// Split the graded gossiper from the rest of the protocol and invoke that when a message is received and save the result for when the hare process starts.
	// Store the early messages and process them when the hare process starts, that means that we'll need to publish those early messages ourselves.
	// Do some processing of early messages and then just do the last bit when the protocol starts.





	shouldRelay, equivocationHash := handler.HandleMsg(msgHash, id, round, values)
	// If we detect a new equivocation then store it.
	if equivocationHash != nil {
		proof := handler.buildMalfeasanceProof(msgHash, *equivocationHash)
		err := b.msh.AddMalfeasanceProof(hareMsg.SmesherID, proof, nil)
		if err != nil {
			logger.With().Error("faild to add newly discovered malfeasance proof to mesh", log.Err(err))
		}
	}
	// If this message shouldn't be relayed return an error to indicate this to the p2p system.
	if !shouldRelay {
		return errors.New("don't relay")
	}

	// Returning nil lets the p2p gossipsub layer know that the received
	// message was valid and it should relay it to the network.
	return nil
}

// getHandler returns the handler for this layer if it has been registered, in
// the case that a handler is registered or the message is considered early
// then the message will also be stored.
func (b *Broker) getHandler(hash types.Hash20, msg *Message, latestLayer types.LayerID) (handler *handler, isEarly bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.handlers[msg.Layer]
	// If the handler has already been registered then return it.
	if h != nil && h.Handler != nil {
		h.storeMessage(hash, msg)
		return h, false
	}

	// If the message is early we want to store it so that it can be processed
	// when a handler is registerd for this layer. Early messagges must be
	// stored before b.mu.Unlock, to ensure that calls to Register see all
	// early messages.
	if msg.Layer == latestLayer+1 {
		// Build the handler if we didn't already
		if h == nil {
			h = newHandler()
			b.handlers[msg.Layer] = h
		}
		h.storeMessage(hash, msg)
		return nil, true
	}
	// No handler and the message wasn't early
	return nil, false
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages.
func (b *Broker) Register(ctx context.Context, id types.LayerID, hareHandler *hare3.Handler) error {
	b.mu.Lock()
	b.mu.Unlock()

	// check to see if the node is still synced
	if !b.Synced(ctx, id) {
		return errInstanceNotSynced
	}

	if !id.After(b.latestLayer) { // should expect to update only newer layer
		return fmt.Errorf("tried to update to a previous layer, broker at: %v, given layer %v", b.latestLayer, id)
	}
	b.latestLayer = id
	h := b.handlers[id]
	if h == nil {
		h = newHandler()
		b.handlers[id] = h
	}
	h.Handler = hareHandler

	// Handle any early messages
	for k, v := range h.messages {
		// Hmm what do we do with early messages in the case of equivocation?
		// Previously we did all the validation with an early message, so we'd only add them if they passed all validation .. we would add them if known malicious, also because we still needed the message.
		b.HandleDecodedMessage(ctx, k, v)
	}

	return nil
}

func (b *Broker) cleanupInstance(id types.LayerID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.handlers, id)
}

// Unregister a layer from receiving messages.
func (b *Broker) Unregister(ctx context.Context, id types.LayerID) {
	b.cleanupInstance(id)
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
