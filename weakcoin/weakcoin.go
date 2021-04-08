package weakcoin

import (
	"fmt"

	"github.com/spacemeshos/amcl"
	"github.com/spacemeshos/amcl/BLS381"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
)

const (
	// DefaultPrefix defines default weak coin proposal prefix.
	DefaultPrefix = "prefix"
	// DefaultThreshold defines default weak coin threshold.
	DefaultThreshold = byte(0x80) // TODO(nkryuchkov): consider using int
	// GossipProtocol is weak coin Gossip protocol name.
	GossipProtocol = "WeakCoinGossip"
)

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
	Log       log.Log
	pk        []byte
	sk        []byte
	signer    *BLS381.BlsSigner
	prefix    string
	threshold byte
	net       broadcaster
	result    bool
}

// NewWeakCoin returns a new WeakCoin.
func NewWeakCoin(prefix string, threshold byte, net broadcaster, logger log.Log) WeakCoin {
	rng := amcl.NewRAND()
	pub := []byte{1}
	rng.Seed(len(pub), []byte{2})
	vrfPriv, vrfPub := BLS381.GenKeyPair(rng)
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)

	wc := &weakCoin{
		Log:       logger,
		pk:        vrfPub,
		sk:        vrfPriv,
		signer:    vrfSigner,
		prefix:    prefix,
		threshold: threshold,
		net:       net,
	}

	return wc
}

func (wc *weakCoin) Get(epoch types.EpochID, round types.RoundID) bool {
	if rand.Intn(2) == 0 {
		return false
	}

	return true
}

func (wc *weakCoin) PublishProposal(epoch types.EpochID, round types.RoundID) error {
	p, err := wc.generateProposal(epoch, round)
	if err != nil {
		return err
	}

	// TODO(nkryuchkov): fix conversion
	serializedMessage, err := types.InterfaceToBytes(p)
	if err != nil {
		return fmt.Errorf("serialize weak coin message: %w", err)
	}

	if err := wc.net.Broadcast(GossipProtocol, serializedMessage); err != nil {
		return fmt.Errorf("broadcast weak coin message: %w", err)
	}

	return nil
}

func (wc *weakCoin) generateProposal(epoch types.EpochID, round types.RoundID) (byte, error) {
	// TODO(nkryuchkov): concat bytes from numbers instead
	msg := []byte(fmt.Sprintf("%s%d%d", wc.prefix, epoch, round))

	result, err := wc.signer.Sign(msg)
	if err != nil {
		return 0, fmt.Errorf("sign message: %w", err)
	}

	// TODO: use another implementation
	sum := byte(0)
	for _, v := range result {
		sum += v
	}

	return sum, nil
}

// Message defines weak coin message format.
type Message struct {
	// TODO(nkryuchkov): implement
}

func (w Message) String() string {
	// TODO(nkryuchkov): implement
	return ""
}
