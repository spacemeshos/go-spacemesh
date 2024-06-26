package types

import (
	"encoding/hex"
	"fmt"
	"time"

	poetShared "github.com/spacemeshos/poet/shared"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen -types PoetProof,PoetProofMessage

type PoetServer struct {
	Address string    `mapstructure:"address" json:"address"`
	Pubkey  Base64Enc `mapstructure:"pubkey" json:"pubkey"`
}

type PoetProofRef Hash32

// EmptyPoetProofRef is an empty PoET proof reference.
var EmptyPoetProofRef = PoetProofRef{}

// PoetProof is the full PoET service proof of elapsed time.
// It includes the number of leaves produced and the actual PoET Merkle proof.
type PoetProof struct {
	poetShared.MerkleProof
	LeafCount uint64
}

func (p *PoetProof) MarshalLogObject(encoder log.ObjectEncoder) error {
	if p == nil {
		return nil
	}
	encoder.AddUint64("LeafCount", p.LeafCount)

	encoder.AddString("MerkleProof.Root", hex.EncodeToString(p.Root))
	encoder.AddArray("MerkleProof.ProvenLeaves", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, v := range p.ProvenLeaves {
			encoder.AppendString(hex.EncodeToString(v))
		}
		return nil
	}))
	encoder.AddArray("MerkleProof.ProofNodes", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, v := range p.ProofNodes {
			encoder.AppendString(hex.EncodeToString(v))
		}
		return nil
	}))

	return nil
}

// PoetProofMessage is the envelope which includes the PoetProof, service ID, round ID and signature.
type PoetProofMessage struct {
	PoetProof
	PoetServiceID []byte `scale:"max=32"` // public key of the PoET service
	RoundID       string `scale:"max=32"` // TODO(mafa): convert to uint64
	// The input to Poet's POSW.
	// It's the root of a merkle tree built from all of the members
	// that are included in the proof.
	Statement Hash32
	Signature EdSignature
}

func (p *PoetProofMessage) MarshalLogObject(encoder log.ObjectEncoder) error {
	if p == nil {
		return nil
	}
	encoder.AddObject("PoetProof", &p.PoetProof)
	encoder.AddString("PoetServiceID", hex.EncodeToString(p.PoetServiceID))
	encoder.AddString("RoundID", p.RoundID)
	encoder.AddString("Statement", hex.EncodeToString(p.Statement[:]))
	encoder.AddString("Signature", p.Signature.String())

	return nil
}

// Ref returns the reference to the PoET proof message. It's the blake3 sum of the entire proof.
func (p *PoetProof) Ref() (PoetProofRef, error) {
	poetProofBytes, err := codec.Encode(p)
	if err != nil {
		return PoetProofRef{}, fmt.Errorf("encoding poet proof: %w", err)
	}
	h := CalcHash32(poetProofBytes)
	return (PoetProofRef)(h), nil
}

// PoetRound includes the PoET's round ID.
type PoetRound struct {
	ID  string `scale:"max=32"`
	End time.Time
}
