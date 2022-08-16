package weakcoin

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	// Prefix defines weak coin proposal prefix.
	proposalPrefix = "WeakCoin"
	// equal to 2^256 / 2.
	defaultThreshold = "0x8000000000000000000000000000000000000000000000000000000000000000"
)

func defaultConfig() config {
	return config{
		Threshold:           util.FromHex(defaultThreshold),
		VRFPrefix:           proposalPrefix,
		NextRoundBufferSize: 10000, // ~1mb given the size of Message is ~100b
		MaxRound:            300,
	}
}

type config struct {
	Threshold           []byte
	VRFPrefix           string
	NextRoundBufferSize int
	MaxRound            types.RoundID
}

// UnitAllowances is a map from miner identifier to the number of units of spacetime.
type UnitAllowances map[string]uint64

// Message defines weak coin message format.
type Message struct {
	Epoch     types.EpochID
	Round     types.RoundID
	Unit      uint64
	MinerPK   []byte
	Signature []byte
}

// EncodeScale implements scale codec interface.
func (t *Message) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Epoch))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Round))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.Unit))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSlice(enc, t.MinerPK)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSlice(enc, t.Signature)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// DecodeScale implements scale codec interface.
func (t *Message) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Epoch = types.EpochID(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Round = types.RoundID(field)
	}
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Unit = uint64(field)
	}
	{
		field, n, err := scale.DecodeByteSlice(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.MinerPK = field
	}
	{
		field, n, err := scale.DecodeByteSlice(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Signature = field
	}
	return total, nil
}

// Option for optional configuration adjustments.
type Option func(*WeakCoin)

// WithLog changes logger.
func WithLog(logger log.Log) Option {
	return func(wc *WeakCoin) {
		wc.logger = logger
	}
}

// WithMaxRound changes max round.
func WithMaxRound(round types.RoundID) Option {
	return func(wc *WeakCoin) {
		wc.config.MaxRound = round
	}
}

// WithThreshold changes signature threshold.
func WithThreshold(threshold []byte) Option {
	return func(wc *WeakCoin) {
		wc.config.Threshold = threshold
	}
}

// WithNextRoundBufferSize changes size of the buffer for messages from future rounds.
func WithNextRoundBufferSize(size int) Option {
	return func(wc *WeakCoin) {
		wc.config.NextRoundBufferSize = size
	}
}

// WithVerifier changes the verifier of the weakcoin messages.
func WithVerifier(v signing.Verifier) Option {
	return func(wc *WeakCoin) {
		wc.verifier = v
	}
}

// New creates an instance of weak coin protocol.
func New(
	publisher pubsub.Publisher,
	signer signing.Signer,
	opts ...Option,
) *WeakCoin {
	wc := &WeakCoin{
		logger:    log.NewNop(),
		config:    defaultConfig(),
		signer:    signer,
		publisher: publisher,
		coins:     make(map[types.RoundID]bool),
	}
	for _, opt := range opts {
		opt(wc)
	}

	if wc.verifier == nil {
		wc.verifier = signing.VRFVerifier{}
	}
	wc.nextRoundBuffer = make([]Message, 0, wc.config.NextRoundBufferSize)
	return wc
}

// WeakCoin implementation of the protocol.
type WeakCoin struct {
	logger    log.Log
	config    config
	verifier  signing.Verifier
	signer    signing.Signer
	publisher pubsub.Publisher

	mu                         sync.RWMutex
	epochStarted, roundStarted bool
	epoch                      types.EpochID
	round                      types.RoundID
	smallestProposal           []byte
	allowances                 UnitAllowances
	// nextRoundBuffer is used to optimistically buffer messages from the next round.
	nextRoundBuffer []Message
	coins           map[types.RoundID]bool
}

// Get the result of the coin flip in this round. It is only valid in between StartEpoch/EndEpoch
// and only after CompleteRound was called.
func (wc *WeakCoin) Get(ctx context.Context, epoch types.EpochID, round types.RoundID) bool {
	if epoch.IsGenesis() {
		wc.logger.WithContext(ctx).With().Panic("beacon weak coin not used during genesis")
	}

	wc.mu.RLock()
	defer wc.mu.RUnlock()
	if wc.epoch != epoch {
		wc.logger.WithContext(ctx).With().Panic("requested epoch wasn't started or already completed",
			log.Uint32("started_epoch", uint32(wc.epoch)),
			log.Uint32("requested_epoch", uint32(epoch)))
	}

	return wc.coins[round]
}

// StartEpoch notifies that epoch is started and we can accept messages for this epoch.
func (wc *WeakCoin) StartEpoch(ctx context.Context, epoch types.EpochID, allowances UnitAllowances) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.epochStarted = true
	wc.epoch = epoch
	wc.allowances = allowances
	wc.logger.WithContext(ctx).With().Info("beacon weak coin started epoch", epoch)
}

// FinishEpoch completes epoch. After it is called Get for this epoch will panic.
func (wc *WeakCoin) FinishEpoch(ctx context.Context, epoch types.EpochID) {
	logger := wc.logger.WithContext(ctx).WithFields(epoch)
	wc.mu.Lock()
	defer wc.mu.Unlock()
	if epoch != wc.epoch {
		logger.Panic("attempted to finish beacon weak coin for the wrong epoch")
	}
	wc.epochStarted = false
	wc.allowances = nil
	wc.coins = map[types.RoundID]bool{}
	wc.round = 0
	logger.Info("weak coin finished epoch")
}

// StartRound process any buffered messages for this round and broadcast our proposal.
func (wc *WeakCoin) StartRound(ctx context.Context, round types.RoundID) error {
	wc.mu.Lock()
	logger := wc.logger.WithContext(ctx).WithFields(wc.epoch, round)
	logger.Info("started beacon weak coin round")
	wc.roundStarted = true
	wc.round = round
	wc.smallestProposal = nil
	epoch := wc.epoch
	for i, msg := range wc.nextRoundBuffer {
		if msg.Epoch != wc.epoch || msg.Round != wc.round {
			continue
		}
		if err := wc.updateProposal(ctx, msg); err != nil {
			logger.With().Debug("received invalid proposal", log.Err(err))
		}
		wc.nextRoundBuffer[i] = Message{}
	}
	wc.nextRoundBuffer = wc.nextRoundBuffer[:0]
	wc.mu.Unlock()

	return wc.publishProposal(ctx, epoch, round)
}

func (wc *WeakCoin) updateProposal(ctx context.Context, message Message) error {
	buf := wc.encodeProposal(message.Epoch, message.Round, message.Unit)
	if !wc.verifier.Verify(signing.NewPublicKey(message.MinerPK), buf, message.Signature) {
		return fmt.Errorf("signature is invalid signature %x", message.Signature)
	}

	allowed, exists := wc.allowances[string(message.MinerPK)]
	if !exists || allowed < message.Unit {
		return fmt.Errorf("miner %x is not allowed to submit proposal for unit %d", message.MinerPK, message.Unit)
	}

	wc.updateSmallest(ctx, message.Signature)
	return nil
}

func (wc *WeakCoin) prepareProposal(epoch types.EpochID, round types.RoundID) (broadcast, smallest []byte) {
	// TODO(dshulyak) double check that 10 means that 10 units are allowed
	allowed, exists := wc.allowances[string(wc.signer.PublicKey().Bytes())]
	if !exists {
		return
	}
	for unit := uint64(0); unit < allowed; unit++ {
		proposal := wc.encodeProposal(epoch, round, unit)
		signature := wc.signer.Sign(proposal)
		if wc.aboveThreshold(signature) {
			continue
		}
		if smallest == nil || bytes.Compare(signature, smallest) == -1 {
			message := Message{
				Epoch:     epoch,
				Round:     round,
				Unit:      unit,
				MinerPK:   wc.signer.PublicKey().Bytes(),
				Signature: signature,
			}
			msg, err := types.InterfaceToBytes(&message)
			if err != nil {
				wc.logger.With().Panic("failed to serialize weak coin message", log.Err(err))
			}

			broadcast = msg
			smallest = signature
		}
	}
	return
}

func (wc *WeakCoin) publishProposal(ctx context.Context, epoch types.EpochID, round types.RoundID) error {
	wc.mu.Lock()
	msg, smallest := wc.prepareProposal(epoch, round)
	wc.mu.Unlock()

	// nothing to send is valid if all proposals are exceeding threshold
	if msg == nil {
		return nil
	}

	if err := wc.publisher.Publish(ctx, pubsub.BeaconWeakCoinProtocol, msg); err != nil {
		return fmt.Errorf("failed to broadcast weak coin message: %w", err)
	}

	wc.logger.WithContext(ctx).With().Info("published proposal",
		epoch,
		round,
		log.String("proposal", types.BytesToHash(smallest).ShortString()))

	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.epoch != epoch || wc.round != round {
		// another epoch/round was started concurrently
		return nil
	}
	wc.updateSmallest(ctx, smallest)
	return nil
}

// FinishRound computes coinflip based on proposals received in this round.
// After it is called new proposals for this round won't be accepted.
func (wc *WeakCoin) FinishRound(ctx context.Context) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	logger := wc.logger.WithContext(ctx).WithFields(wc.epoch, wc.round)
	wc.roundStarted = false
	if wc.smallestProposal == nil {
		logger.Warning("completed round without valid proposals")
		return
	}
	// NOTE(dshulyak) we need to select good bit here. for ed25519 it means select LSB and never MSB.
	// https://datatracker.ietf.org/doc/html/draft-josefsson-eddsa-ed25519-03#section-5.2
	// For another signature algorithm this may change
	lsbIndex := 0
	if !wc.signer.LittleEndian() {
		lsbIndex = len(wc.smallestProposal) - 1
	}
	coinflip := wc.smallestProposal[lsbIndex]&1 == 1

	wc.coins[wc.round] = coinflip
	logger.With().Info("completed round with beacon weak coin",
		log.String("proposal", types.BytesToHash(wc.smallestProposal).ShortString()),
		log.Bool("beacon_weak_coin", coinflip))
	wc.smallestProposal = nil
}

func (wc *WeakCoin) updateSmallest(ctx context.Context, proposal []byte) {
	if len(proposal) > 0 && (bytes.Compare(proposal, wc.smallestProposal) == -1 || wc.smallestProposal == nil) {
		wc.logger.WithContext(ctx).With().Debug("saving new proposal",
			wc.epoch,
			wc.round,
			log.String("proposal", types.BytesToHash(proposal).ShortString()),
			log.String("previous", types.BytesToHash(wc.smallestProposal).ShortString()),
		)
		wc.smallestProposal = proposal
	}
}

func (wc *WeakCoin) aboveThreshold(proposal []byte) bool {
	return bytes.Compare(proposal, wc.config.Threshold) == 1
}

func (wc *WeakCoin) encodeProposal(epoch types.EpochID, round types.RoundID, unit uint64) []byte {
	proposal := bytes.Buffer{}
	proposal.WriteString(wc.config.VRFPrefix)
	if _, err := proposal.Write(epoch.ToBytes()); err != nil {
		wc.logger.With().Panic("can't write epoch to a buffer", log.Err(err))
	}
	roundBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(roundBuf, uint64(round))
	if _, err := proposal.Write(roundBuf); err != nil {
		wc.logger.With().Panic("can't write uint64 to a buffer", log.Err(err))
	}
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, unit)
	if _, err := proposal.Write(buf); err != nil {
		wc.logger.With().Panic("can't write uint64 to a buffer", log.Err(err))
	}
	return proposal.Bytes()
}
