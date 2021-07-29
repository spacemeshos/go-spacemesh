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
	"github.com/spacemeshos/go-spacemesh/p2p/service"
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
}

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

	mu         sync.RWMutex
	epoch      types.EpochID
	round      types.RoundID
	vrf        []byte
	allowances UnitAllowances
	coins      map[types.RoundID]bool
	// nextRoundBuffer is used to optimistically buffer messages from the next round.
	// we will avoid problems with some messages being lost simply because one node was slightly
	// faster then the other.
	// TODO(dshulyak) append to it in broadcast handler, and iterate over when StartRound is called
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
	return wc.coins[round]
}

func (wc *WeakCoin) publishProposal(ctx context.Context) error {
	wc.mu.RLock()
	wc.logger.With().Debug("publishing proposal",
		log.Uint32("epoch_id", uint32(wc.epoch)),
		log.Uint64("round_id", uint64(wc.round)))
	var (
		broadcast []byte
		smallest  []byte
		epoch     = wc.epoch
		round     = wc.round
	)
	wc.mu.RUnlock()

	for unit := uint64(1); unit <= wc.allowances[string(wc.signer.PublicKey().Bytes())]; unit++ {
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
				wc.logger.Panic("can't serialize weak coin", log.Err(err))
			}

			broadcast = msg
			smallest = signature
		}

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
	wc.updateVrf(smallest)
	return nil
}

func (wc *WeakCoin) updateVrf(proposal []byte) {
	if len(proposal) > 0 && (bytes.Compare(proposal, wc.vrf) == -1 || wc.vrf == nil) {
		wc.vrf = proposal
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
	wc.coins = map[types.RoundID]bool{}
}

func (wc *WeakCoin) StartRound(ctx context.Context, round types.RoundID) error {
	wc.mu.Lock()
	wc.logger.With().Info("round started",
		log.Uint32("epoch_id", uint32(wc.epoch)),
		log.Uint64("round_id", uint64(round)))
	wc.round = round
	wc.vrf = nil
	wc.mu.Unlock()
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
	wc.coins[wc.round] = coinflip
	wc.logger.With().Info("completed round",
		log.Uint32("epoch_id", uint32(wc.epoch)),
		log.Uint64("round_id", uint64(wc.round)),
		log.String("smallest", hex.EncodeToString(wc.vrf)),
		log.Bool("coinflip", coinflip))
	wc.round = 0
	wc.vrf = nil
}
