package weakcoin

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/spacemeshos/amcl"
	"github.com/spacemeshos/amcl/BLS381"
	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

const (
	// DefaultPrefix defines default weak coin proposal prefix.
	DefaultPrefix = "prefix"
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
type WeakCoin interface {
	Get(epoch types.EpochID, round types.RoundID) bool
	PublishProposal(epoch types.EpochID, round types.RoundID) error
	OnRoundStarted(epoch types.EpochID, round types.RoundID)
	OnRoundFinished(epoch types.EpochID, round types.RoundID)
	HandleSerializedMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher)
}

type weakCoin struct {
	Log            log.Log
	pk             []byte
	sk             []byte
	signer         *BLS381.BlsSigner
	prefix         string
	threshold      *big.Int
	net            broadcaster
	proposalsMu    sync.RWMutex
	proposals      map[epochRoundPair][]types.Hash32
	activeRoundsMu sync.RWMutex
	activeRounds   map[epochRoundPair]struct{}
	weakCoinsMu    sync.RWMutex
	weakCoins      map[epochRoundPair]bool
}

// NewWeakCoin returns a new WeakCoin.
func NewWeakCoin(prefix string, threshold types.Hash32, net broadcaster, logger log.Log) WeakCoin {
	rng := amcl.NewRAND()
	pub := []byte{1}
	rng.Seed(len(pub), []byte{2})
	vrfPriv, vrfPub := BLS381.GenKeyPair(rng)
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)
	thresholdBigInt := new(big.Int).SetBytes(threshold[:])

	wc := &weakCoin{
		Log:          logger,
		pk:           vrfPub,
		sk:           vrfPriv,
		signer:       vrfSigner,
		prefix:       prefix,
		threshold:    thresholdBigInt,
		net:          net,
		proposals:    make(map[epochRoundPair][]types.Hash32),
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

func (wc *weakCoin) PublishProposal(epoch types.EpochID, round types.RoundID) error {
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

	// TODO(nkryuchkov): fix conversion
	serializedMessage, err := types.InterfaceToBytes(message)
	if err != nil {
		return fmt.Errorf("serialize weak coin message: %w", err)
	}

	// TODO(nkryuchkov): pass correct context
	if err := wc.net.Broadcast(context.Background(), GossipProtocol, serializedMessage); err != nil {
		return fmt.Errorf("broadcast weak coin message: %w", err)
	}

	return nil
}

func (wc *weakCoin) proposalExceedsThreshold(proposal types.Hash32) bool {
	proposalInt := new(big.Int).SetBytes(proposal[:])

	return proposalInt.Cmp(wc.threshold) == 1
}

func (wc *weakCoin) generateProposal(epoch types.EpochID, round types.RoundID) (types.Hash32, error) {
	msg := bytes.Buffer{}

	msg.WriteString(wc.prefix)

	epochBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBuf, uint64(epoch))
	if _, err := msg.Write(epochBuf); err != nil {
		return types.Hash32{}, err
	}

	roundBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(roundBuf, uint64(round))
	if _, err := msg.Write(roundBuf); err != nil {
		return types.Hash32{}, err
	}

	vrfSig, err := wc.signer.Sign(msg.Bytes())
	if err != nil {
		return types.Hash32{}, fmt.Errorf("sign message: %w", err)
	}

	vrfHash := sha256.Sum256(vrfSig)

	return vrfHash, nil
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
