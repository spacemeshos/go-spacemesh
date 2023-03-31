package types

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/spacemeshos/go-scale"
	poetShared "github.com/spacemeshos/poet/shared"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen

type PoetChallenge struct {
	*NIPostChallenge
	InitialPost         *Post
	InitialPostMetadata *PostMetadata
	NumUnits            uint32
}

func (c *PoetChallenge) MarshalLogObject(encoder log.ObjectEncoder) error {
	if c == nil {
		return nil
	}
	if err := encoder.AddObject("NIPostChallenge", c.NIPostChallenge); err != nil {
		return err
	}
	if err := encoder.AddObject("InitialPost", c.InitialPost); err != nil {
		return err
	}
	if err := encoder.AddObject("InitialPostMetadata", c.InitialPostMetadata); err != nil {
		return err
	}
	encoder.AddUint32("NumUnits", c.NumUnits)
	return nil
}

type Member [32]byte

// EncodeScale implements scale codec interface.
func (m *Member) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, m[:])
}

// DecodeScale implements scale codec interface.
func (m *Member) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, m[:])
}

type PoetProofRef [32]byte

// EmptyPoetProofRef is an empty PoET proof reference.
var EmptyPoetProofRef = PoetProofRef{}

// PoetProof is the full PoET service proof of elapsed time. It includes the list of members, a leaf count declaration
// and the actual PoET Merkle proof.
type PoetProof struct {
	poetShared.MerkleProof
	Members   []Member `scale:"max=100000"` // max size depends on how many smeshers submit a challenge to a poet server
	LeafCount uint64
}

func (p *PoetProof) MarshalLogObject(encoder log.ObjectEncoder) error {
	if p == nil {
		return nil
	}
	encoder.AddUint64("LeafCount", p.LeafCount)
	encoder.AddArray("Indices", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, member := range p.Members {
			encoder.AppendString(hex.EncodeToString(member[:]))
		}
		return nil
	}))

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
	Signature     EdSignature
}

func (p *PoetProofMessage) MarshalLogObject(encoder log.ObjectEncoder) error {
	if p == nil {
		return nil
	}
	encoder.AddObject("PoetProof", &p.PoetProof)
	encoder.AddString("PoetServiceID", hex.EncodeToString(p.PoetServiceID))
	encoder.AddString("RoundID", p.RoundID)
	encoder.AddString("Signature", p.Signature.String())

	return nil
}

// Ref returns the reference to the PoET proof message. It's the blake3 sum of the entire proof message.
func (proofMessage *PoetProofMessage) Ref() (PoetProofRef, error) {
	poetProofBytes, err := codec.Encode(&proofMessage.PoetProof)
	if err != nil {
		return PoetProofRef{}, fmt.Errorf("failed to marshal poet proof for poetId %x round %v: %w",
			proofMessage.PoetServiceID, proofMessage.RoundID, err)
	}
	h := CalcHash32(poetProofBytes)
	return (PoetProofRef)(h), nil
}

type RoundEnd time.Time

func (re RoundEnd) Equal(other RoundEnd) bool {
	return (time.Time)(re).Equal((time.Time)(other))
}

func (re *RoundEnd) IntoTime() time.Time {
	return (time.Time)(*re)
}

func (p *RoundEnd) EncodeScale(enc *scale.Encoder) (total int, err error) {
	t := p.IntoTime()
	n, err := scale.EncodeString(enc, t.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return n, nil
}

// DecodeScale implements scale codec interface.
func (p *RoundEnd) DecodeScale(dec *scale.Decoder) (total int, err error) {
	field, n, err := scale.DecodeString(dec)
	if err != nil {
		return 0, err
	}
	t, err := time.Parse(time.RFC3339Nano, field)
	if err != nil {
		return n, err
	}
	*p = (RoundEnd)(t)
	return n, nil
}

// PoetRound includes the PoET's round ID.
type PoetRound struct {
	ID            string `scale:"max=32"`
	ChallengeHash Hash32
	End           RoundEnd
}

// ProcessingError is a type of error (implements the error interface) that is used to differentiate processing errors
// from validation errors.
type ProcessingError struct {
	Err string `scale:"max=1024"` // TODO(mafa): make error code instead of string
}

// Error returns the processing error as a string. It implements the error interface.
func (s ProcessingError) Error() string {
	return s.Err
}
