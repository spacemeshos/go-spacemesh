package tortoisebeacon

import (
	"fmt"

	"github.com/spacemeshos/amcl"
	"github.com/spacemeshos/amcl/BLS381"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	defaultPrefix    = "prefix"
	defaultThreshold = byte(0x80) // TODO(nkryuchkov): consider using int
)

// WeakCoinGenerator generates weak coin.
type WeakCoinGenerator interface {
	Publish(epoch types.EpochID, round uint64) error
}

type weakCoinGenerator struct {
	pk        []byte
	sk        []byte
	signer    *BLS381.BlsSigner
	prefix    string
	threshold byte
	net       broadcaster
}

// NewWeakCoinGenerator returns a new weakCoinGenerator.
func NewWeakCoinGenerator(prefix string, threshold byte, net broadcaster) WeakCoinGenerator {
	rng := amcl.NewRAND()
	pub := []byte{1}
	rng.Seed(len(pub), []byte{2})
	vrfPriv, vrfPub := BLS381.GenKeyPair(rng)
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)

	wcg := &weakCoinGenerator{
		pk:        vrfPub,
		sk:        vrfPriv,
		signer:    vrfSigner,
		prefix:    prefix,
		threshold: threshold,
		net:       net,
	}

	return wcg
}

func (wcg *weakCoinGenerator) Publish(epoch types.EpochID, round uint64) error {
	p, err := wcg.generateProposal(epoch, round)
	if err != nil {
		return err
	}

	// TODO(nkryuchkov): fix conversion
	serializedMessage, err := types.InterfaceToBytes(p)
	if err != nil {
		return err
	}

	if err := wcg.net.Broadcast(TBWeakCoinProtocol, serializedMessage); err != nil {
		return err
	}

	return nil
}

func (wcg *weakCoinGenerator) generateProposal(epoch types.EpochID, round uint64) (byte, error) {
	// TODO(nkryuchkov): concat bytes from numbers instead
	msg := []byte(fmt.Sprintf("%s%d%d", wcg.prefix, epoch, round))
	result, err := wcg.signer.Sign(msg)
	if err != nil {
		return 0, err
	}

	// TODO: use another implementation
	sum := byte(0)
	for _, v := range result {
		sum += v
	}

	return sum, nil
}

// WeakCoinMessage defines weak coin message format.
type WeakCoinMessage struct {
	// TODO(nkryuchkov): implement
}

func (w WeakCoinMessage) String() string {
	// TODO(nkryuchkov): implement
	return ""
}
