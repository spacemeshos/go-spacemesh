package weakcoin

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	// Prefix defines weak coin proposal prefix.
	proposalPrefix = "WeakCoin"
	// GossipProtocol is weak coin Gossip protocol name.
	GossipProtocol = "WeakCoinGossip"

	defaultThreshold = "0x8000000000000000000000000000000000000000000000000000000000000000"
)

func DefaultConfig() Config {
	return Config{
		Threshold:           util.FromHex(defaultThreshold),
		VRFPrefix:           proposalPrefix,
		NextRoundBufferSize: 10000, // ~1mb given the size of Message is ~100b
	}
}

type Config struct {
	Threshold           []byte
	VRFPrefix           string
	NextRoundBufferSize int
	MaxRound            types.RoundID
}

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./weak_coin.go

type broadcaster interface {
	Broadcast(ctx context.Context, channel string, data []byte) error
}

// UnitAllowances is a map from miner identifier to the number of units of spacetime.
type UnitAllowances map[string]uint64

// Message defines weak coin message format.
type Message struct {
	Epoch     types.EpochID
	Round     types.RoundID
	Unit      uint64
	Signature []byte
}

type Option func(*WeakCoin)

func WithLog(logger log.Log) Option {
	return func(wc *WeakCoin) {
		wc.logger = logger
	}
}

func WithConfig(config Config) Option {
	return func(wc *WeakCoin) {
		wc.config = config
	}
}

// New creates an instance of weak coin protocol.
func New(
	net broadcaster,
	signer signing.Signer,
	verifier signing.Verifier,
	opts ...Option,
) *WeakCoin {
	wc := &WeakCoin{
		logger:   log.NewNop(),
		config:   DefaultConfig(),
		signer:   signer,
		verifier: verifier,
		net:      net,
		coins:    make(map[types.RoundID]bool),
	}
	for _, opt := range opts {
		opt(wc)
	}
	wc.nextRoundBuffer = make([]Message, 0, wc.config.NextRoundBufferSize)
	return wc
}

type WeakCoin struct {
	logger   log.Log
	config   Config
	verifier signing.Verifier
	signer   signing.Signer
	net      broadcaster

	mu               sync.RWMutex
	epoch            types.EpochID
	round            types.RoundID
	smallestProposal []byte
	allowances       UnitAllowances
	// nextRoundBuffer is used to optimistically buffer messages from the next round.
	nextRoundBuffer []Message
	coins           map[types.RoundID]bool
}

// Get the result of the coinflip in this round. It is only valid inbetween StartEpoch/EndEpoch
// and only after CompleteRound was called.
func (wc *WeakCoin) Get(epoch types.EpochID, round types.RoundID) bool {
	if epoch.IsGenesis() {
		return false
	}

	wc.mu.RLock()
	defer wc.mu.RUnlock()
	if wc.epoch != epoch {
		wc.logger.Panic("requested epoch wasn't started or already completed",
			log.Uint32("started_epoch", uint32(wc.epoch)),
			log.Uint32("requested_epoch", uint32(epoch)))
	}

	return wc.coins[round]
}

func (wc *WeakCoin) StartEpoch(epoch types.EpochID, allowances UnitAllowances) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.epoch = epoch
	wc.allowances = allowances
}

func (wc *WeakCoin) CompleteEpoch() {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.allowances = nil
	wc.coins = map[types.RoundID]bool{}
	wc.round = 0
}

func (wc *WeakCoin) StartRound(ctx context.Context, round types.RoundID) error {
	wc.mu.Lock()
	wc.logger.With().Info("started round",
		log.Uint32("epoch_id", uint32(wc.epoch)),
		log.Uint64("round_id", uint64(round)))
	wc.round = round
	wc.smallestProposal = nil
	epoch := wc.epoch
	for i, msg := range wc.nextRoundBuffer {
		if msg.Epoch != wc.epoch || msg.Round != wc.round {
			continue
		}
		if err := wc.updateProposal(msg); err != nil {
			wc.logger.With().Debug("received invalid proposal",
				log.Err(err),
				log.Uint32("epoch_id", uint32(msg.Epoch)),
				log.Uint64("round_id", uint64(msg.Round)))
		}
		wc.nextRoundBuffer[i].Epoch = 0
		wc.nextRoundBuffer[i].Round = 0
		wc.nextRoundBuffer[i].Unit = 0
		wc.nextRoundBuffer[i].Signature = nil
	}
	wc.nextRoundBuffer = wc.nextRoundBuffer[:0]
	wc.mu.Unlock()
	return wc.publishProposal(ctx, epoch, round)
}

func (wc *WeakCoin) updateProposal(message Message) error {
	buf := wc.encodeProposal(message.Epoch, message.Round, message.Unit)
	miner, err := wc.verifier.Extract(buf, message.Signature)
	if err != nil {
		return fmt.Errorf("can't recover miner id from signature: %w", err)
	}
	allowed, exists := wc.allowances[string(miner.Bytes())]
	if !exists || allowed < message.Unit {
		return fmt.Errorf("miner %x is not allowed to submit proposal for unit %d", miner, message.Unit)
	}
	wc.updateSmallest(message.Signature)
	return nil
}

func (wc *WeakCoin) publishProposal(ctx context.Context, epoch types.EpochID, round types.RoundID) error {
	var (
		broadcast []byte
		smallest  []byte
		// TODO(dshulyak) double check that 10 means that 10 units are allowed
		allowed, exists = wc.allowances[string(wc.signer.PublicKey().Bytes())]
	)
	if !exists {
		return nil
	}
	for unit := uint64(0); unit < allowed; unit++ {
		proposal := wc.encodeProposal(epoch, round, unit)
		signature := wc.signer.Sign(proposal)
		if wc.exceedsThreshold(signature) {
			continue
		}
		if smallest == nil || bytes.Compare(signature, smallest) == -1 {
			message := Message{
				Epoch:     epoch,
				Round:     round,
				Unit:      unit,
				Signature: signature,
			}
			msg, err := types.InterfaceToBytes(message)
			if err != nil {
				wc.logger.Panic("can't serialize weak coin message", log.Err(err))
			}

			broadcast = msg
			smallest = signature
		}
	}
	// nothing to send is valid if all proposals are exceeding threshold
	if broadcast == nil {
		return nil
	}

	if err := wc.net.Broadcast(ctx, GossipProtocol, broadcast); err != nil {
		return fmt.Errorf("failed to broadcast weak coin message: %w", err)
	}

	wc.logger.With().Info("published proposal",
		log.Uint32("epoch_id", uint32(epoch)),
		log.Uint64("round_id", uint64(round)),
		log.String("proposal", types.BytesToHash(smallest).ShortString()))

	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.epoch != epoch || wc.round != round {
		// another epoch/round was started concurrently
		return nil
	}
	wc.updateSmallest(smallest)
	return nil
}

func (wc *WeakCoin) CompleteRound() {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.smallestProposal == nil {
		wc.logger.With().Error("completed round without valid proposals",
			log.Uint32("epoch_id", uint32(wc.epoch)),
			log.Uint64("round_id", uint64(wc.round)),
		)
		return
	}
	coinflip := wc.smallestProposal[0]&1 == 1

	wc.coins[wc.round] = coinflip
	wc.logger.With().Info("completed round",
		log.Uint32("epoch_id", uint32(wc.epoch)),
		log.Uint64("round_id", uint64(wc.round)),
		log.String("smallest", hex.EncodeToString(wc.smallestProposal)),
		log.Bool("coinflip", coinflip))
	wc.smallestProposal = nil
}

func (wc *WeakCoin) updateSmallest(proposal []byte) {
	if len(proposal) > 0 && (bytes.Compare(proposal, wc.smallestProposal) == -1 || wc.smallestProposal == nil) {
		wc.logger.Debug("saving new proposal",
			log.Uint32("epoch_id", uint32(wc.epoch)),
			log.Uint64("round_id", uint64(wc.round)),
			log.String("proposal", types.BytesToHash(proposal).ShortString()))
		wc.smallestProposal = proposal
	}
}

func (wc *WeakCoin) exceedsThreshold(proposal []byte) bool {
	return bytes.Compare(proposal, wc.config.Threshold) == 1
}

func (wc *WeakCoin) encodeProposal(epoch types.EpochID, round types.RoundID, unit uint64) []byte {
	proposal := bytes.Buffer{}
	proposal.WriteString(wc.config.VRFPrefix)
	if _, err := proposal.Write(epoch.ToBytes()); err != nil {
		wc.logger.Panic("can't write epoch to a buffer", log.Err(err))
	}
	roundBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(roundBuf, uint64(round))
	if _, err := proposal.Write(roundBuf); err != nil {
		wc.logger.Panic("can't write uint64 to a buffer", log.Err(err))
	}
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, unit)
	if _, err := proposal.Write(buf); err != nil {
		wc.logger.Panic("can't write uint16 to a buffer", log.Err(err))
	}
	return proposal.Bytes()
}
