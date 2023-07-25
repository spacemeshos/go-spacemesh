package broker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare3/weakcoin"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

var errNilInner = errors.New("nil inner message")

type HandlerFactory struct {
	clock         *timesync.NodeClock
	roundTime     time.Duration
	hareThreshold uint16
	l             log.Log
}

func NewHandlerFactory(clock *timesync.NodeClock, roundTime time.Duration, hareThreshold uint16, l log.Log) *HandlerFactory {
	return &HandlerFactory{
		clock:         clock,
		roundTime:     roundTime,
		hareThreshold: hareThreshold,
		l:             l,
	}
}

func (f *HandlerFactory) Handler(layer types.LayerID) *hare3.Handler {
	return hare3.NewHandler(
		hare3.NewDefaultGradedGossiper(),
		hare3.NewDefaultThresholdGradedGossiper(f.hareThreshold),
		hare3.NewDefaultGradecaster(),
		f.l,
	)
}

func newLayerState(handlerFactory *HandlerFactory, layer types.LayerID) *layerState {
	return &layerState{
		messages: make(map[types.Hash20]*hare.Message),
		// TODO actually build the handler
		handler:     handlerFactory.Handler(layer),
		coinChooser: weakcoin.NewChooser(),
	}
}

// layerState is a container to simplify keeping track of the state for a given layer.
type layerState struct {
	// Stores all the messages that were not dropped by the handler
	messages    map[types.Hash20]*hare.Message
	handler     *hare3.Handler
	coinChooser *weakcoin.Chooser
}

func (s *layerState) buildMalfeasanceProof(a, b types.Hash20) *types.MalfeasanceProof {
	proofMsg := func(hash types.Hash20) types.HareProofMsg {
		msg := s.messages[hash]

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
		Layer: s.messages[a].Layer,
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

	cdb                  *datastore.CachedDB
	edVerifier           *signing.EdVerifier
	eligibilityValidator *eligibility.Validator
	handlerFactory       *HandlerFactory
	latestLayer          types.LayerID // the latest layer to attempt register (successfully or unsuccessfully)
	layerStates          map[types.LayerID]*layerState

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func NewBroker(
	cdb *datastore.CachedDB,
	edVerifier *signing.EdVerifier,
	eligibilityValidator *eligibility.Validator,
	handlerFactory *HandlerFactory,
	log log.Log,
) *Broker {
	b := &Broker{
		Log:                  log,
		cdb:                  cdb,
		edVerifier:           edVerifier,
		eligibilityValidator: eligibilityValidator,
		handlerFactory:       handlerFactory,
		layerStates:          make(map[types.LayerID]*layerState),
		latestLayer:          types.GetEffectiveGenesis(),
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

var errClosed = errors.New("closed")

func parts(msg *hare.Message) (id types.NodeID, round hare3.AbsRound, values []types.Hash20) {
	values = make([]types.Hash20, len(msg.Values))
	for i := range msg.Values {
		values[i] = types.Hash20(msg.Values[i])
	}
	return msg.SmesherID, hare3.AbsRound(msg.Round), values
}

func compareProposals(values []types.ProposalID) func(i, j int) bool {
	return func(i, j int) bool {
		return bytes.Compare(values[i][:], values[j][:]) == -1
	}
}

func sortProposalID(values []types.ProposalID) []types.ProposalID {
	sort.Slice(values, compareProposals(values))
	return values
}

func ToHash(values []types.ProposalID) types.ProposalID {
	h := sha256.New()
	for _, v := range values {
		h.Write(v[:])
	}
	var result types.ProposalID
	copy(result[:], h.Sum(nil))
	return result
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
// equivocating message for that round.
//
// An equivocating message is considered to be either a message from a
// pre-known malfeasant identity (one for which we had a malfeasance proof) or
// any messages beyond the first for a given identity in a given round.
func (b *Broker) HandleMessage(ctx context.Context, _ p2p.Peer, msg []byte) error {
	select {
	case <-ctx.Done():
		return errClosed
	case <-b.ctx.Done():
		return errClosed
	default:
	}

	received := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	hash := types.CalcMessageHash20(msg, pubsub.HareProtocol)
	hareMsg, err := hare.MessageFromBuffer(msg)
	if err != nil {
		b.Debug("failed to decode message hash %q error: %v", hash.ToHash12(), err)
		return err
	}
	if hareMsg.InnerMessage == nil {
		b.Debug("hare msg missing inner msg")
		return errNilInner
	}
	vh := ToHash(sortProposalID(hareMsg.Values))
	msgString := fmt.Sprintf("h: %s, l: %d, r: %d, s: %s, vh: %s", hash.ShortString(), hareMsg.Layer, hareMsg.Round, hareMsg.SmesherID.ShortString(), vh)
	logger := b.WithFields(log.String("msg", msgString))

	if !sort.SliceIsSorted(hareMsg.Values, compareProposals(hareMsg.Values)) {
		b.Debug("hare msg values not sorted")
		return errors.New("values not sorted")
	}

	logger.Debug("received hare message")

	state := b.layerStates[hareMsg.Layer]

	early := hareMsg.Layer == b.latestLayer+1
	// Exit now if this message is not early and not for a registered layer
	if state == nil && !early {
		logger.Debug("consensus process not registered")
		return errors.New("consensus process not registered")
	}
	if state != nil {
		_, ok := state.messages[hash]
		if ok {
			// This is a warning, because p2p pubsub should not push duplicate messages.
			logger.Warning("duplicate message received")
			return errors.New("duplicate message received")
		}
	}

	if !b.edVerifier.Verify(signing.HARE, hareMsg.SmesherID, hareMsg.SignedBytes(), hareMsg.Signature) {
		logger.Debug("failed to verify signature",
			log.Int("sig_len", len(hareMsg.Signature)),
		)
		return fmt.Errorf("verify ed25519 signature")
	}

	id, round, values := parts(hareMsg)
	proof, err := b.cdb.GetMalfeasanceProof(hareMsg.SmesherID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		// An error here indicates a database failure.
		logger.Panic("failed to check malicious identity: %v", err)
	}

	if proof != nil {
		logger.Debug("msg is from malicious identity")
		// The way we signify a message from a known malfeasant identity to the
		// protocol is a message without values.
		values = nil
	}

	// Check whether the message is eligible
	valid, err := b.eligibilityValidator.Validate(ctx, hareMsg.Eligibility.Proof, hareMsg.SmesherID, hareMsg.Layer, hare3.AbsRound(hareMsg.Round), hareMsg.Eligibility.Count)
	if err != nil {
		err := fmt.Errorf("failed to validate eligibility: %v", err)
		logger.Debug("%v", err)
		return err
	}
	if !valid {
		err := errors.New("message contained incorrect eligibility count")
		logger.Debug("%v", err)
		return err
	}

	// If the message is early we may need to initialize the message store and handler.
	if early && state == nil {
		state = newLayerState(b.handlerFactory, b.latestLayer)
		b.layerStates[hareMsg.Layer] = state
	}

	logger.Debug("broker passing message to hare handler")
	shouldRelay, equivocationHash := state.handler.HandleMsg(hash, id, round, values, 1)
	// If we detect a new equivocation then store it.
	if equivocationHash != nil {
		proof = state.buildMalfeasanceProof(hash, *equivocationHash)

		encoded, err := codec.Encode(proof)
		if err != nil {
			logger.Panic("failed to encode MalfeasanceProof: %v", err)
		}
		if err := identities.SetMalicious(b.cdb, hareMsg.SmesherID, encoded, received); err != nil {
			// An error here indicates a database failure
			logger.Panic("faild to store newly discovered malfeasance proof: %v", err)
		}
		b.cdb.CacheMalfeasanceProof(hareMsg.SmesherID, proof)
	}

	// If this message shouldn't be relayed (this is a message from an identity
	// for which we previously detected equivocation) return an error to
	// indicate this to the p2p system.
	if !shouldRelay {
		logger.Debug("broker not relaying")
		return errors.New("don't relay")
	}

	// The weak coin is calculated from pre-round messages, we are not overly
	// concerned about equivocations affecting the outcome since the coin is
	// weak. (see package doc for hare3/weakcoin for an explanation of weak)
	// However since at this point we know if the message was an equivocation
	// based on the proof variable, we filter it anyway to save a few cycles.
	if proof == nil && hare3.AbsRound(hareMsg.Round).Type() == hare3.Preround {
		state.coinChooser.Put(&hareMsg.Eligibility.Proof)
	}

	// Only store messages that get relayed
	state.messages[hash] = hareMsg

	// Returning nil lets the p2p gossipsub layer know that the received
	// message was valid and it should relay it to the network.
	return nil
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages.
func (b *Broker) Register(ctx context.Context, id types.LayerID) (*hare3.Handler, *weakcoin.Chooser) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.latestLayer = id
	state := b.layerStates[id]
	if state == nil {
		state = newLayerState(b.handlerFactory, b.latestLayer)
		b.layerStates[id] = state
	} else {
		b.handleEarlyMessages(state.messages, state.handler)
	}

	// we return the handler so that we can extract the relevant sub protocols
	// from it for use by the main protocol.
	return state.handler, state.coinChooser
}

// Malfeasance detections that happened in the previous layer are not passed to
// the early layer, so we need to re-handle early messages in case their sender
// was detected to be malfeasant in the previous layer. This will just update
// the value held for the message in the protocol in the case that a sender has
// been detected to be malfeasant since the early message was processed.
func (b *Broker) handleEarlyMessages(msgs map[types.Hash20]*hare.Message, handler *hare3.Handler) {
	for k, v := range msgs {
		proof, err := b.cdb.GetMalfeasanceProof(v.SmesherID)
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			b.With().Panic("re-handling early messages, failed to check malicious identity", log.Stringer("smesher", v.SmesherID), log.Err(err))
		}
		if proof != nil {
			id, round, _ := parts(v)
			// The way we signify a message from a know malfeasant identity to the
			// protocol is a message without values.
			handler.HandleMsg(k, id, round, nil, 1)
		}
	}
}

// Unregister a layer from receiving messages.
func (b *Broker) Unregister(ctx context.Context, id types.LayerID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.layerStates, id)
	b.WithContext(ctx).With().Debug("hare broker unregistered layer", id)
}

// Close closes broker.
func (b *Broker) Close() {
	b.cancel()
}
