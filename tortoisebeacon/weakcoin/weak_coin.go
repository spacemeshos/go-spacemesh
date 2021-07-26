package weakcoin

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ALTree/bigfloat"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

const (
	// Prefix defines weak coin proposal prefix.
	proposalPrefix = "WeakCoin"
	// GossipProtocol is weak coin Gossip protocol name.
	GossipProtocol = "WeakCoinGossip"
)

// DefaultThreshold defines default weak coin threshold.
// It's equal to (2^256) / 2
// (hex: 0x8000000000000000000000000000000000000000000000000000000000000000)
var DefaultThreshold, _ = new(big.Float).Quo(
	bigfloat.Pow(
		new(big.Float).SetInt64(2),
		new(big.Float).SetInt64(256),
	),
	new(big.Float).SetInt64(2),
).Int(nil)

type epochRoundPair struct {
	EpochID types.EpochID
	Round   types.RoundID
}

type broadcaster interface {
	Broadcast(ctx context.Context, channel string, data []byte) error
}

// WeakCoin defines weak coin interface.
// The line below generates mocks for it.
//go:generate mockery -name WeakCoin -case underscore -inpkg
type WeakCoin interface {
	Get(epoch types.EpochID, round types.RoundID) bool
	PublishProposal(ctx context.Context, epoch types.EpochID, round types.RoundID) error
	OnRoundStarted(epoch types.EpochID, round types.RoundID)
	OnRoundFinished(epoch types.EpochID, round types.RoundID)
	HandleSerializedMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher)
}

type weakCoin struct {
	Log            log.Log
	minerID        types.NodeID
	verifier       verifierFunc
	signer         signer
	threshold      *big.Int
	net            broadcaster
	proposalsMu    sync.RWMutex
	proposals      map[epochRoundPair][][]byte
	activeRoundsMu sync.RWMutex
	activeRounds   map[epochRoundPair]struct{}
	weakCoinsMu    sync.RWMutex
	weakCoins      map[epochRoundPair]bool
}

// a function to verify the message with the signature and its public key.
type verifierFunc = func(pub, msg, sig []byte) bool

type signer interface {
	Sign(msg []byte) []byte
}

// NewWeakCoin returns a new WeakCoin.
func NewWeakCoin(
	threshold *big.Int,
	minerID types.NodeID,
	net broadcaster,
	vrfVerifier verifierFunc,
	vrfSigner signer,
	logger log.Log,
) WeakCoin {
	wc := &weakCoin{
		Log:          logger,
		minerID:      minerID,
		verifier:     vrfVerifier,
		signer:       vrfSigner,
		threshold:    threshold,
		net:          net,
		proposals:    make(map[epochRoundPair][][]byte),
		activeRounds: make(map[epochRoundPair]struct{}),
		weakCoins:    make(map[epochRoundPair]bool),
	}

	return wc
}

func (wc *weakCoin) Get(epoch types.EpochID, round types.RoundID) bool {
	if epoch.IsGenesis() {
		return false
	}

	pair := epochRoundPair{
		EpochID: epoch,
		Round:   round,
	}

	wc.weakCoinsMu.RLock()
	defer wc.weakCoinsMu.RUnlock()

	return wc.weakCoins[pair]
}

func (wc *weakCoin) PublishProposal(ctx context.Context, epoch types.EpochID, round types.RoundID) error {
	wc.Log.With().Info("Publishing proposal",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)))

	proposal, err := wc.generateProposal(epoch, round)
	if err != nil {
		return err
	}

	signedProposal := wc.signProposal(proposal)

	if wc.proposalExceedsThreshold(proposal) {
		// If a proposal exceeds the threshold, it is not sent.
		return nil
	}

	message := Message{
		Epoch:        epoch,
		Round:        round,
		MinerID:      wc.minerID,
		VRFSignature: signedProposal,
	}

	serializedMessage, err := types.InterfaceToBytes(message)
	if err != nil {
		return fmt.Errorf("serialize weak coin message: %w", err)
	}

	if err := wc.net.Broadcast(ctx, GossipProtocol, serializedMessage); err != nil {
		return fmt.Errorf("broadcast weak coin message: %w", err)
	}

	wc.Log.With().Info("Published proposal",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)),
		log.String("proposal", types.BytesToHash(signedProposal).ShortString()))

	return nil
}

func (wc *weakCoin) proposalExceedsThreshold(proposal []byte) bool {
	proposalInt := new(big.Int).SetBytes(proposal)

	return proposalInt.Cmp(wc.threshold) == 1
}

func (wc *weakCoin) generateProposal(epoch types.EpochID, round types.RoundID) ([]byte, error) {
	proposal := bytes.Buffer{}

	proposal.WriteString(proposalPrefix)

	epochBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBuf, uint64(epoch))
	if _, err := proposal.Write(epochBuf); err != nil {
		return nil, err
	}

	roundBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(roundBuf, uint64(round))
	if _, err := proposal.Write(roundBuf); err != nil {
		return nil, err
	}

	return proposal.Bytes(), nil
}

func (wc *weakCoin) signProposal(proposal []byte) []byte {
	return wc.signer.Sign(proposal)
}

func (wc *weakCoin) OnRoundStarted(epoch types.EpochID, round types.RoundID) {
	wc.Log.With().Info("Round started",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)))

	wc.activeRoundsMu.Lock()
	defer wc.activeRoundsMu.Unlock()

	pair := epochRoundPair{
		EpochID: epoch,
		Round:   round,
	}

	if _, ok := wc.activeRounds[pair]; !ok {
		wc.activeRounds[pair] = struct{}{}
	}
}

func (wc *weakCoin) OnRoundFinished(epoch types.EpochID, round types.RoundID) {
	wc.Log.With().Info("Round finished",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)))

	wc.activeRoundsMu.Lock()
	defer wc.activeRoundsMu.Unlock()

	pair := epochRoundPair{
		EpochID: epoch,
		Round:   round,
	}

	if _, ok := wc.activeRounds[pair]; ok {
		delete(wc.activeRounds, pair)
		wc.calculateWeakCoin(epoch, round)
	}
}

func (wc *weakCoin) calculateWeakCoin(epoch types.EpochID, round types.RoundID) {
	pair := epochRoundPair{
		EpochID: epoch,
		Round:   round,
	}

	wc.proposalsMu.RLock()
	defer wc.proposalsMu.RUnlock()

	wc.weakCoinsMu.Lock()
	defer wc.weakCoinsMu.Unlock()

	proposals := wc.proposals[pair]

	if len(proposals) == 0 {
		wc.Log.With().Warning("No weak coin proposals were received",
			log.Uint64("epoch_id", uint64(epoch)),
			log.Uint64("round_id", uint64(round)))

		wc.weakCoins[pair] = false

		return
	}

	smallest := new(big.Int).SetBytes(proposals[0])
	stringProposals := []string{smallest.String()}

	for i := 1; i < len(proposals); i++ {
		current := new(big.Int).SetBytes(proposals[i])
		stringProposals = append(stringProposals, current.String())

		if current.Cmp(smallest) == -1 {
			smallest = current
		}
	}

	coinBit := new(big.Int).And(smallest, big.NewInt(1))

	weakCoinValue := coinBitToBool(coinBit)
	wc.weakCoins[pair] = weakCoinValue

	wc.Log.With().Info("Weak coin calculation input",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)),
		log.String("smallest", smallest.String()),
		log.String("coin_bit", coinBit.String()),
		log.String("proposals", strings.Join(stringProposals, ", ")))

	wc.Log.With().Info("Calculated weak coin",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)),
		log.Bool("weak_coin", weakCoinValue))
}

func coinBitToBool(coinBit *big.Int) bool {
	switch coinBit.Int64() {
	case 0:
		return false
	case 1:
		return true
	default:
		panic("bad input")
	}
}
