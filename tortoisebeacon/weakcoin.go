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

type weakCoinMessagePublisher interface {
	PublishWeakCoinMessage(message byte) error
}

type WeakCoinGenerator struct {
	pk        []byte
	sk        []byte
	signer    *BLS381.BlsSigner
	prefix    string
	threshold byte
	wcmp      weakCoinMessagePublisher
}

func NewWeakCoinGenerator(prefix string, threshold byte, wcmp weakCoinMessagePublisher) *WeakCoinGenerator {
	rng := amcl.NewRAND()
	pub := []byte{1}
	rng.Seed(len(pub), []byte{2})
	vrfPriv, vrfPub := BLS381.GenKeyPair(rng)
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)

	wcg := &WeakCoinGenerator{
		pk:        vrfPub,
		sk:        vrfPriv,
		signer:    vrfSigner,
		prefix:    prefix,
		threshold: threshold,
		wcmp:      wcmp,
	}

	return wcg
}

func (wcg *WeakCoinGenerator) Publish(epoch types.EpochID, round int) error {
	p, err := wcg.GenerateProposal(epoch, round)
	if err != nil {
		return err
	}

	if err := wcg.wcmp.PublishWeakCoinMessage(p); err != nil {
		return err
	}

	return nil
}

func (wcg *WeakCoinGenerator) GenerateProposal(epoch types.EpochID, round int) (byte, error) {
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
