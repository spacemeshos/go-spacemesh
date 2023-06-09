package weakcoin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

var (
	errNotGenerated = errors.New("weakcoin not generated")
	errNotSmallest  = errors.New("proposal not smallest")
)

func defaultConfig() config {
	return config{
		NextRoundBufferSize: 10000, // ~1mb given the size of Message is ~100b
		MaxRound:            300,
		Threshold: types.VrfSignature{
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,

			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		},
	}
}

type config struct {
	Threshold           types.VrfSignature
	NextRoundBufferSize int
	MaxRound            types.RoundID
}

//go:generate scalegen -types Message,VrfMessage

// Message defines weak coin message format.
type Message struct {
	Epoch        types.EpochID
	Round        types.RoundID
	Unit         uint32
	NodeID       types.NodeID
	VRFSignature types.VrfSignature
}

// VrfMessage is the payload for the signature of `Message`.
type VrfMessage struct {
	Type  types.EligibilityType // always types.EligibilityBeaconWC
	Nonce types.VRFPostIndex
	Epoch types.EpochID
	Round types.RoundID
	Unit  uint32
}

// OptionFunc for optional configuration adjustments.
type OptionFunc func(*WeakCoin)

// WithLog changes logger.
func WithLog(logger log.Log) OptionFunc {
	return func(wc *WeakCoin) {
		wc.logger = logger
	}
}

// WithMaxRound changes max round.
func WithMaxRound(round types.RoundID) OptionFunc {
	return func(wc *WeakCoin) {
		wc.config.MaxRound = round
	}
}

// WithThreshold changes signature threshold.
func WithThreshold(threshold types.VrfSignature) OptionFunc {
	return func(wc *WeakCoin) {
		wc.config.Threshold = threshold
	}
}

// WithNextRoundBufferSize changes size of the buffer for messages from future rounds.
func WithNextRoundBufferSize(size int) OptionFunc {
	return func(wc *WeakCoin) {
		wc.config.NextRoundBufferSize = size
	}
}

// messageTime interface exists so that we can pass an object from the beacon
// package to the weakCoinPackage (as does allowance), this is indicative of a
// circular dependency, probably the weak coin should be merged with the beacon
// package.
// Issue: https://github.com/spacemeshos/go-spacemesh/issues/4199
type messageTime interface {
	WeakCoinProposalSendTime(epoch types.EpochID, round types.RoundID) time.Time
}

// New creates an instance of weak coin protocol.
func New(
	publisher pubsub.Publisher,
	signer vrfSigner,
	verifier vrfVerifier,
	nonceFetcher nonceFetcher,
	allowance allowance,
	msgTime messageTime,
	opts ...OptionFunc,
) *WeakCoin {
	wc := &WeakCoin{
		logger:       log.NewNop(),
		config:       defaultConfig(),
		signer:       signer,
		nonceFetcher: nonceFetcher,
		allowance:    allowance,
		publisher:    publisher,
		coins:        make(map[types.RoundID]bool),
		verifier:     verifier,
		msgTime:      msgTime,
	}
	for _, opt := range opts {
		opt(wc)
	}

	wc.nextRoundBuffer = make([]Message, 0, wc.config.NextRoundBufferSize)
	return wc
}

// WeakCoin implementation of the protocol.
type WeakCoin struct {
	logger       log.Log
	config       config
	verifier     vrfVerifier
	signer       vrfSigner
	nonceFetcher nonceFetcher
	publisher    pubsub.Publisher

	mu                         sync.RWMutex
	epochStarted, roundStarted bool
	epoch                      types.EpochID
	round                      types.RoundID
	smallest                   *types.VrfSignature
	allowance                  allowance
	// nextRoundBuffer is used to optimistically buffer messages from the next round.
	nextRoundBuffer []Message
	coins           map[types.RoundID]bool
	msgTime         messageTime
}

// Get the result of the coin flip in this round. It is only valid in between StartEpoch/EndEpoch
// and only after CompleteRound was called.
func (wc *WeakCoin) Get(ctx context.Context, epoch types.EpochID, round types.RoundID) (bool, error) {
	if epoch.FirstLayer() <= types.GetEffectiveGenesis() {
		wc.logger.WithContext(ctx).With().Fatal("beacon weak coin not used during genesis")
	}

	wc.mu.RLock()
	defer wc.mu.RUnlock()
	if wc.epoch != epoch {
		wc.logger.WithContext(ctx).With().Fatal("requested epoch wasn't started or already completed",
			log.Uint32("started_epoch", uint32(wc.epoch)),
			log.Uint32("requested_epoch", uint32(epoch)))
	}

	flip, ok := wc.coins[round]
	if !ok {
		return false, errNotGenerated
	}
	return flip, nil
}

// StartEpoch notifies that epoch is started and we can accept messages for this epoch.
func (wc *WeakCoin) StartEpoch(ctx context.Context, epoch types.EpochID) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.epochStarted = true
	wc.epoch = epoch
	wc.logger.WithContext(ctx).With().Info("beacon weak coin started epoch", epoch)
}

// FinishEpoch completes epoch.
func (wc *WeakCoin) FinishEpoch(ctx context.Context, epoch types.EpochID) {
	logger := wc.logger.WithContext(ctx).WithFields(epoch)
	wc.mu.Lock()
	defer wc.mu.Unlock()
	if epoch != wc.epoch {
		logger.With().Fatal("attempted to finish beacon weak coin for the wrong epoch",
			epoch,
			log.Stringer("weak_coin_epoch", wc.epoch),
		)
	}
	wc.epochStarted = false
	wc.coins = map[types.RoundID]bool{}
	wc.round = 0
	logger.Info("weak coin finished epoch")
}

// StartRound process any buffered messages for this round and broadcast our proposal.
func (wc *WeakCoin) StartRound(ctx context.Context, round types.RoundID, nonce *types.VRFPostIndex) {
	wc.mu.Lock()
	logger := wc.logger.WithContext(ctx).WithFields(wc.epoch, round)
	logger.Info("started beacon weak coin round")
	wc.roundStarted = true
	wc.round = round
	for i, msg := range wc.nextRoundBuffer {
		if msg.Epoch != wc.epoch || msg.Round != wc.round {
			continue
		}
		if err := wc.updateProposal(ctx, msg); err != nil && !errors.Is(err, errNotSmallest) {
			logger.With().Warning("invalid weakcoin proposal", log.Err(err))
		}
		wc.nextRoundBuffer[i] = Message{}
	}
	wc.nextRoundBuffer = wc.nextRoundBuffer[:0]
	wc.mu.Unlock()

	if nonce != nil {
		wc.publishProposal(ctx, wc.epoch, *nonce, wc.round)
	}
}

func (wc *WeakCoin) updateProposal(ctx context.Context, message Message) error {
	nonce, err := wc.nonceFetcher.VRFNonce(message.NodeID, message.Epoch)
	if err != nil {
		wc.logger.With().Error("failed to get vrf nonce", log.Err(err))
		return fmt.Errorf("failed to get vrf nonce for node %s: %w", message.NodeID, err)
	}
	buf := wc.encodeProposal(message.Epoch, nonce, message.Round, message.Unit)
	if !wc.verifier.Verify(message.NodeID, buf, message.VRFSignature) {
		return fmt.Errorf("signature is invalid signature %x", message.VRFSignature)
	}

	allowance := wc.allowance.MinerAllowance(wc.epoch, message.NodeID)
	if allowance < message.Unit {
		return fmt.Errorf("miner %x is not allowed to submit proposal for unit %d (allowed %d)", message.NodeID, message.Unit, allowance)
	}

	return wc.updateSmallest(ctx, message.VRFSignature)
}

func (wc *WeakCoin) prepareProposal(epoch types.EpochID, nonce types.VRFPostIndex, round types.RoundID) ([]byte, types.VrfSignature) {
	minerAllowance := wc.allowance.MinerAllowance(wc.epoch, wc.signer.NodeID())
	if minerAllowance == 0 {
		return nil, types.EmptyVrfSignature
	}
	var broadcast []byte
	var smallest *types.VrfSignature
	for unit := uint32(0); unit < minerAllowance; unit++ {
		proposal := wc.encodeProposal(epoch, nonce, round, unit)
		signature := wc.signer.Sign(proposal)
		if wc.aboveThreshold(signature) {
			continue
		}
		if signature.Cmp(smallest) == -1 {
			message := Message{
				Epoch:        epoch,
				Round:        round,
				Unit:         unit,
				NodeID:       wc.signer.NodeID(),
				VRFSignature: signature,
			}
			msg, err := codec.Encode(&message)
			if err != nil {
				wc.logger.With().Fatal("failed to serialize weak coin message", log.Err(err))
			}

			broadcast = msg
			smallest = &signature
		}
	}

	wc.mu.RLock()
	defer wc.mu.RUnlock()
	if smallest == nil || smallest.Cmp(wc.smallest) != -1 {
		return nil, types.EmptyVrfSignature
	}
	return broadcast, *smallest
}

func (wc *WeakCoin) publishProposal(ctx context.Context, epoch types.EpochID, nonce types.VRFPostIndex, round types.RoundID) {
	msg, proposal := wc.prepareProposal(epoch, nonce, round)
	if msg == nil {
		return
	}

	if err := wc.publisher.Publish(ctx, pubsub.BeaconWeakCoinProtocol, msg); err != nil {
		wc.logger.With().Warning("failed to publish own weak coin proposal",
			epoch,
			round,
			log.Stringer("proposal", proposal),
			log.Err(err),
		)
		return
	}

	wc.logger.WithContext(ctx).With().Info("published proposal",
		epoch,
		round,
		log.Stringer("proposal", proposal),
	)
}

// FinishRound computes coinflip based on proposals received in this round.
// After it is called new proposals for this round won't be accepted.
func (wc *WeakCoin) FinishRound(ctx context.Context) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	logger := wc.logger.WithContext(ctx).WithFields(wc.epoch, wc.round)
	wc.roundStarted = false
	if wc.smallest == nil {
		logger.Warning("completed round without valid proposals")
		return
	}
	coinflip := wc.smallest.LSB()&1 == 1

	wc.coins[wc.round] = coinflip
	logger.With().Info("completed round with beacon weak coin",
		log.Stringer("proposal", wc.smallest),
		log.Bool("beacon_weak_coin", coinflip),
	)
	wc.smallest = nil
}

func (wc *WeakCoin) updateSmallest(ctx context.Context, sig types.VrfSignature) error {
	if sig.Cmp(wc.smallest) == -1 {
		wc.logger.WithContext(ctx).With().Debug("saving new proposal",
			wc.epoch,
			wc.round,
			log.Stringer("proposal", sig),
			log.Stringer("previous", wc.smallest),
		)
		wc.smallest = &sig
		return nil
	}
	return errNotSmallest
}

func (wc *WeakCoin) aboveThreshold(proposal types.VrfSignature) bool {
	return proposal.Cmp(&wc.config.Threshold) == 1
}

func (wc *WeakCoin) encodeProposal(epoch types.EpochID, nonce types.VRFPostIndex, round types.RoundID, unit uint32) []byte {
	message := &VrfMessage{
		Type:  types.EligibilityBeaconWC,
		Nonce: nonce,
		Epoch: epoch,
		Round: round,
		Unit:  unit,
	}

	b, err := codec.Encode(message)
	if err != nil {
		wc.logger.With().Fatal("failed to encode weak coin vrf msg", log.Err(err))
	}
	return b
}
