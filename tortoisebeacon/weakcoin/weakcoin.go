package weakcoin

import (
	"bytes"
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
	Broadcast(channel string, data []byte) error
}

// WeakCoin defines weak coin interface.
type WeakCoin interface {
	Get(epoch types.EpochID, round types.RoundID) bool
	PublishProposal(epoch types.EpochID, round types.RoundID) error
	HandleSerializedMessage(data service.GossipMessage, sync service.Fetcher)
}

type weakCoin struct {
	Log         log.Log
	pk          []byte
	sk          []byte
	signer      *BLS381.BlsSigner
	prefix      string
	threshold   *big.Int
	net         broadcaster
	proposalsMu sync.RWMutex
	proposals   map[epochRoundPair][]types.Hash32
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
		Log:       logger,
		pk:        vrfPub,
		sk:        vrfPriv,
		signer:    vrfSigner,
		prefix:    prefix,
		threshold: thresholdBigInt,
		net:       net,
		proposals: make(map[epochRoundPair][]types.Hash32),
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

	wc.proposalsMu.RLock()
	defer wc.proposalsMu.RUnlock()

	if len(wc.proposals[pair]) == 0 {
		// TODO(nkryuchkov): return error or wait
		return false
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

	return result.Cmp(big.NewInt(1)) == 0
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

	if err := wc.net.Broadcast(GossipProtocol, serializedMessage); err != nil {
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

// Message defines weak coin message format.
type Message struct {
	Epoch    types.EpochID
	Round    types.RoundID
	Proposal types.Hash32
}

func (w Message) String() string {
	return fmt.Sprintf("%v/%v/%v", w.Epoch, w.Round, w.Proposal)
}
