package weakcoin

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	// Prefix defines weak coin proposal prefix.
	proposalPrefix = "WeakCoin"
	// GossipProtocol is weak coin Gossip protocol name.
	GossipProtocol = "WeakCoinGossip"

	defaultThreshold = "8000000000000000000000000000000000000000000000000000000000000000"
)

type broadcaster interface {
	Broadcast(ctx context.Context, channel string, data []byte) error
}

// Coin defines weak coin interface.
// The line below generates mocks for it.
//go:generate mockery --name Coin --case underscore --output ./mocks/
type Coin interface {
	StartEpoch(types.EpochID, UnitAllowances)
	StartRound(context.Context, types.RoundID) error
	CompleteRound()
	Get(types.RoundID) bool
	CompleteEpoch()
	HandleSerializedMessage(context.Context, service.GossipMessage, service.Fetcher)
}

// UnitAllowances is a map from miner identifier to the number of units of spacetime.
type UnitAllowances map[string]uint64

// Message defines weak coin message format.
type Message struct {
	Epoch     types.EpochID
	Round     types.RoundID
	Unit      uint16
	Signature []byte
}

type Option func(*WeakCoin)

func WithThreshold(buf []byte) Option {
	return func(wc *WeakCoin) {
		wc.threshold = buf
	}
}

func WithLog(logger log.Log) Option {
	return func(wc *WeakCoin) {
		wc.logger = logger
	}
}

func WithNextRoundBufferSize(size int) Option {
	return func(wc *WeakCoin) {
		wc.nextRoundBufSize = size
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
		logger:    log.NewNop(),
		signer:    signer,
		verifier:  verifier,
		net:       net,
		weakCoins: make(map[types.RoundID]bool),
	}
	for _, opt := range opts {
		opt(wc)
	}
	wc.nextRoundBuffer = make([]Message, 0, wc.nextRoundBufSize)
	return wc
}

type WeakCoin struct {
	logger           log.Log
	verifier         signing.Verifier
	signer           signing.Signer
	threshold        []byte
	nextRoundBufSize int
	net              broadcaster

	mu         sync.RWMutex
	epoch      types.EpochID
	round      types.RoundID
	vrf        []byte
	allowances UnitAllowances
	weakCoins  map[types.RoundID]bool
	// nextRoundBuffer is used to to optimistically buffer messages in the next round.
	// we will avoid problems with some messages being lost simply because one node was slightly
	// faster then the other.
	// TODO(dshulyak) append to it in broadcast handler, and iterator over when StartRound is called
	nextRoundBuffer []Message
}

// Get the result of the coinflip in this round. It is only valid inbetween StartEpoch/EndEpoch
// and only after CompleteRound was called.
func (wc *WeakCoin) Get(round types.RoundID) bool {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	if wc.epoch.IsGenesis() {
		return false
	}
	return wc.weakCoins[round]
}

func (wc *WeakCoin) publishProposal(ctx context.Context) error {
	wc.logger.With().Debug("publishing proposal",
		log.Uint32("epoch_id", uint32(wc.epoch)),
		log.Uint64("round_id", uint64(wc.round)))
	var (
		broadcast []byte
		smallest  []byte
	)
	for unit := uint64(1); unit <= wc.allowances[string(wc.signer.PublicKey().Bytes())]; unit++ {
		proposal := wc.encodeProposal(wc.epoch, wc.round, uint16(unit))
		signature := wc.signer.Sign(proposal)
		if wc.exceedsThreshold(signature) {
			// If a proposal exceeds the threshold, it is not sent.
			continue
		}

		message := Message{
			Epoch:     wc.epoch,
			Round:     wc.round,
			Unit:      uint16(unit),
			Signature: signature,
		}
		msg, err := types.InterfaceToBytes(message)
		if err != nil {
			wc.logger.Panic("can't serialize weak coin", log.Err(err))
		}
		if bytes.Compare(signature, smallest) == -1 {
			broadcast = msg
			smallest = signature
		}
	}
	if err := wc.net.Broadcast(ctx, GossipProtocol, broadcast); err != nil {
		return fmt.Errorf("failed to broadcast weak coin message: %w", err)
	}
	wc.logger.With().Info("published proposal",
		log.Uint32("epoch_id", uint32(wc.epoch)),
		log.Uint64("round_id", uint64(wc.round)),
		log.String("proposal", types.BytesToHash(smallest).ShortString()))

	if smallest != nil && bytes.Compare(smallest, wc.vrf) == -1 {
		wc.vrf = smallest
	}
	return nil
}

func (wc *WeakCoin) exceedsThreshold(proposal []byte) bool {
	return bytes.Compare(proposal, wc.threshold) == 1
}

func (wc *WeakCoin) encodeProposal(epoch types.EpochID, round types.RoundID, unit uint16) []byte {
	proposal := bytes.Buffer{}
	proposal.WriteString(proposalPrefix)
	if _, err := proposal.Write(epoch.ToBytes()); err != nil {
		wc.logger.Panic("can't write to a buffer epoch value", log.Err(err))
	}
	roundBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(roundBuf, uint64(round))
	if _, err := proposal.Write(roundBuf); err != nil {
		wc.logger.Panic("can't write to a buffer uint64", log.Err(err))
	}
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, unit)
	if _, err := proposal.Write(buf); err != nil {
		wc.logger.Panic("can't write to a buffer uint16", log.Err(err))
	}
	return proposal.Bytes()
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
	wc.epoch = 0
	wc.allowances = nil
	wc.weakCoins = map[types.RoundID]bool{}
}

func (wc *WeakCoin) StartRound(ctx context.Context, round types.RoundID) error {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.logger.With().Info("round started",
		log.Uint32("epoch_id", uint32(wc.epoch)),
		log.Uint64("round_id", uint64(round)))

	wc.round = round
	wc.vrf = nil
	return wc.publishProposal(ctx)
}

func (wc *WeakCoin) CompleteRound() {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.vrf == nil {
		return
		// TODO(dshulyak) consider to panic here. if used correctly this should never be called twice
		// in the same round.
		// wc.logger.Panic("round wasn't started")
	}

	coinflip := wc.vrf[0]&1 == 1
	wc.weakCoins[wc.round] = coinflip
	wc.logger.With().Info("completed round",
		log.Uint32("epoch_id", uint32(wc.epoch)),
		log.Uint64("round_id", uint64(wc.round)),
		log.String("smallest", hex.EncodeToString(wc.vrf)),
		log.Bool("coinflip", coinflip))
	wc.round = 0
	wc.vrf = nil
}
