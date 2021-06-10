package weakcoin

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"sync"

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

var (
	// DefaultThreshold defines default weak coin threshold.
	DefaultThreshold = types.HexToHash32("0x80" + strings.Repeat("00", 31)) // 0x80...00
)

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
type verifierFunc = func(msg, sig, pub []byte) (bool, error)

type signer interface {
	Sign(msg []byte) ([]byte, error)
}

// NewWeakCoin returns a new WeakCoin.
func NewWeakCoin(
	threshold types.Hash32,
	net broadcaster,
	vrfVerifier verifierFunc,
	vrfSigner signer,
	logger log.Log,
) WeakCoin {
	thresholdBigInt := new(big.Int).SetBytes(threshold[:])

	wc := &weakCoin{
		Log:          logger,
		verifier:     vrfVerifier,
		signer:       vrfSigner,
		threshold:    thresholdBigInt,
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
	p, err := wc.generateProposal(epoch, round)
	if err != nil {
		return err
	}

	if wc.proposalExceedsThreshold(p) {
		// If a proposal exceeds the threshold, it is not sent.
		return nil
	}

	message := Message{
		Epoch:    epoch,
		Round:    round,
		Proposal: p,
	}

	serializedMessage, err := types.InterfaceToBytes(message)
	if err != nil {
		return fmt.Errorf("serialize weak coin message: %w", err)
	}

	if err := wc.net.Broadcast(ctx, GossipProtocol, serializedMessage); err != nil {
		return fmt.Errorf("broadcast weak coin message: %w", err)
	}

	return nil
}

func (wc *weakCoin) proposalExceedsThreshold(proposal []byte) bool {
	proposalInt := new(big.Int).SetBytes(proposal[:])

	return proposalInt.Cmp(wc.threshold) == 1
}

func (wc *weakCoin) generateProposal(epoch types.EpochID, round types.RoundID) ([]byte, error) {
	msg := bytes.Buffer{}

	msg.WriteString(proposalPrefix)

	epochBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBuf, uint64(epoch))
	if _, err := msg.Write(epochBuf); err != nil {
		return nil, err
	}

	roundBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(roundBuf, uint64(round))
	if _, err := msg.Write(roundBuf); err != nil {
		return nil, err
	}

	vrfSig, err := wc.signer.Sign(msg.Bytes())
	if err != nil {
		return nil, fmt.Errorf("sign message: %w", err)
	}

	return vrfSig, nil
}

func (wc *weakCoin) OnRoundStarted(epoch types.EpochID, round types.RoundID) {
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

	if len(wc.proposals[pair]) == 0 {
		wc.Log.Warning("No weak coin proposals were received",
			log.Uint64("epoch_id", uint64(epoch)),
			log.Uint64("round", uint64(round)))

		wc.weakCoins[pair] = false

		return
	}

	smallest := new(big.Int).SetBytes(wc.proposals[pair][0][:])

	for i := 1; i < len(wc.proposals[pair]); i++ {
		current := new(big.Int).SetBytes(wc.proposals[pair][i][:])

		if current.Cmp(smallest) == -1 {
			smallest = current
		}
	}

	result := &big.Int{}
	result.And(smallest, big.NewInt(1))

	coin := result.Cmp(big.NewInt(1)) == 1
	wc.weakCoins[pair] = coin

	wc.Log.With().Info("Calculated weak coin",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(round)),
		log.Bool("weak_coin", coin))
}
