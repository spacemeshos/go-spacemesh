package types

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	// BallotIDSize in bytes.
	// FIXME(dshulyak) why do we cast to hash32 when returning bytes?
	BallotIDSize = Hash32Length
)

//go:generate scalegen

// BallotID is a 20-byte sha256 sum of the serialized ballot used to identify a Ballot.
type BallotID Hash20

// EmptyBallotID is a canonical empty BallotID.
var EmptyBallotID = BallotID{}

// EncodeScale implements scale codec interface.
func (id *BallotID) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, id[:])
}

// DecodeScale implements scale codec interface.
func (id *BallotID) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, id[:])
}

// Ballot contains the smesher's signed vote on the mesh history.
type Ballot struct {
	// InnerBallot is a signed part of the ballot.
	InnerBallot
	// smesher's signature on InnerBallot
	Signature []byte
	// Votes field is not signed.
	Votes Votes

	// the following fields are kept private and from being serialized
	ballotID BallotID
	// the public key of the smesher used
	smesherID *signing.PublicKey
	// malicious is set to true if smesher that produced this ballot is known to be malicious.
	malicious bool
}

// InnerBallot contains all info about a smesher's votes on the mesh history. this structure is
// serialized and signed to produce the signature in Ballot.
type InnerBallot struct {
	// the smesher's ATX in the epoch this ballot is cast.
	AtxID ATXID
	// the proof of the smesher's eligibility to vote and propose block content in this epoch.
	// Eligibilities must be produced in the ascending order.
	EligibilityProofs []VotingEligibilityProof
	// OpinionHash is a aggregated opinion on all previous layers.
	// It is included into transferred data explicitly, so that signature
	// can be verified before decoding votes.
	OpinionHash Hash32

	// the first Ballot the smesher cast in the epoch. this Ballot is a special Ballot that contains information
	// that cannot be changed mid-epoch.
	RefBallot BallotID
	EpochData *EpochData

	// the layer ID in which this ballot is eligible for. this will be validated via EligibilityProof
	LayerIndex LayerID
}

// Votes is for encoding local votes to send over the wire.
//
// a smesher creates votes in the following steps:
// - select a Ballot in the past as a base Ballot
// - calculate the opinion difference on history between the smesher and the base Ballot
// - encode the opinion difference in 3 list:
//   - ForDiff
//     contains blocks we support while the base ballot did not support (i.e. voted against)
//     for blocks we support in layers later than the base ballot, we also add them to this list
//   - AgainstDiff
//     contains blocks we vote against while the base ballot explicitly supported
//   - NeutralDiff
//     contains layers we vote neutral while the base ballot explicitly supported or voted against
//
// example:
// layer | unified content block
// -----------------------------------------------------------------------------------------------
//
//	N   | UCB_A (genesis)
//
// -----------------------------------------------------------------------------------------------
//
//	N+1  | UCB_B base:UCB_A, for:[UCB_A], against:[], neutral:[]
//
// -----------------------------------------------------------------------------------------------
//
//	N+2  | UCB_C base:UCB_B, for:[UCB_B], against:[], neutral:[]
//
// -----------------------------------------------------------------------------------------------
//
//	(hare hasn't terminated for N+2)
//	N+3  | UCB_D base:UCB_B, for:[UCB_B], against:[], neutral:[N+2]
//
// -----------------------------------------------------------------------------------------------
//
//	(hare succeeded for N+2 but failed for N+3)
//	N+4  | UCB_E base:UCB_C, for:[UCB_C], against:[], neutral:[]
//
// -----------------------------------------------------------------------------------------------
// NOTE on neutral votes: a base block is by default neutral on all blocks and layers that come after it, so
// there's no need to explicitly add neutral votes for more recent layers.
//
// TODO: maybe collapse Support and Against into a single list.
//
//	see https://github.com/spacemeshos/go-spacemesh/issues/2369.
type Votes struct {
	// Base ballot.
	Base BallotID
	// Support and Against blocks that base ballot votes differently.
	Support, Against []BlockID
	// Abstain on layers until they are terminated.
	Abstain []LayerID
}

// MarshalLogObject implements logging interface.
func (v *Votes) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("base", v.Base.String())
	encoder.AddArray("support", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, bid := range v.Support {
			encoder.AppendString(bid.String())
		}
		return nil
	}))
	encoder.AddArray("against", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, bid := range v.Against {
			encoder.AppendString(bid.String())
		}
		return nil
	}))
	encoder.AddArray("abstain", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, lid := range v.Abstain {
			encoder.AppendString(lid.String())
		}
		return nil
	}))
	return nil
}

// Opinion is a tuple from opinion hash and votes that decode to opinion hash.
type Opinion struct {
	Hash Hash32
	Votes
}

// MarshalLogObject implements logging interface.
func (o *Opinion) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("hash", o.Hash.String())
	return o.Votes.MarshalLogObject(encoder)
}

// EpochData contains information that cannot be changed mid-epoch.
type EpochData struct {
	// from the smesher's view, the set of ATXs eligible to vote and propose block content in this epoch
	ActiveSet []ATXID
	// the beacon value the smesher recorded for this epoch
	Beacon Beacon
}

// VotingEligibilityProof includes the required values that, along with the smesher's VRF public key,
// allow non-interactive voting eligibility validation. this proof provides eligibility for both voting and
// making proposals.
type VotingEligibilityProof struct {
	// the counter value used to generate this eligibility proof. if the value of J is 3, this is the smesher's
	// eligibility proof of the 3rd ballot/proposal in the epoch.
	J uint32
	// the VRF signature of some epoch specific data and J. one can derive a Ballot's layerID from this signature.
	Sig []byte
}

// MarshalLogObject implements logging interface.
func (v *VotingEligibilityProof) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("j", v.J)
	encoder.AddString("sig", util.Bytes2Hex(v.Sig))
	return nil
}

// Initialize calculates and sets the Ballot's cached ballotID and smesherID.
// this should be called once all the other fields of the Ballot are set.
func (b *Ballot) Initialize() error {
	if b.ID() != EmptyBallotID {
		return fmt.Errorf("ballot already initialized")
	}
	if b.Signature == nil {
		return fmt.Errorf("cannot calculate Ballot ID: signature is nil")
	}

	hasher := hash.New()
	_, err := codec.EncodeTo(hasher, &b.InnerBallot)
	if err != nil {
		return fmt.Errorf("failed to encode inner ballot for hashing")
	}
	_, err = codec.EncodeByteSlice(hasher, b.Signature)
	if err != nil {
		return fmt.Errorf("failed to encode byte slice")
	}
	b.ballotID = BallotID(BytesToHash(hasher.Sum(nil)).ToHash20())

	data := b.SignedBytes()
	pubkey, err := signing.ExtractPublicKey(data, b.Signature)
	if err != nil {
		return fmt.Errorf("ballot extract key: %w", err)
	}
	b.smesherID = signing.NewPublicKey(pubkey)
	return nil
}

// SignedBytes returns the serialization of the InnerBallot for signing.
func (b *Ballot) SignedBytes() []byte {
	data, err := codec.Encode(&b.InnerBallot)
	if err != nil {
		log.Panic("failed to serialize ballot: %v", err)
	}
	return data
}

// SetID from stored data.
func (b *Ballot) SetID(id BallotID) {
	b.ballotID = id
}

// ID returns the BallotID.
func (b *Ballot) ID() BallotID {
	return b.ballotID
}

// SetSmesherID from stored data.
func (b *Ballot) SetSmesherID(id *signing.PublicKey) {
	b.smesherID = id
}

// SmesherID returns the smesher's Edwards public key.
func (b *Ballot) SmesherID() *signing.PublicKey {
	return b.smesherID
}

// SetMalicious sets ballot as malicious.
func (b *Ballot) SetMalicious() {
	b.malicious = true
}

// IsMalicious returns true if ballot is malicious.
func (b *Ballot) IsMalicious() bool {
	return b.malicious
}

// MarshalLogObject implements logging encoder for Ballot.
func (b *Ballot) MarshalLogObject(encoder log.ObjectEncoder) error {
	var (
		activeSetSize = 0
		beacon        Beacon
	)

	if b.EpochData != nil {
		activeSetSize = len(b.EpochData.ActiveSet)
		beacon = b.EpochData.Beacon
	}

	encoder.AddString("ballot_id", b.ID().String())
	encoder.AddUint32("layer_id", b.LayerIndex.Value)
	encoder.AddUint32("epoch_id", uint32(b.LayerIndex.GetEpoch()))
	encoder.AddString("smesher", b.SmesherID().String())
	encoder.AddString("opinion hash", b.OpinionHash.String())
	encoder.AddString("base_ballot", b.Votes.Base.String())
	encoder.AddInt("support", len(b.Votes.Support))
	encoder.AddInt("against", len(b.Votes.Against))
	encoder.AddInt("abstain", len(b.Votes.Abstain))
	encoder.AddString("atx_id", b.AtxID.String())
	encoder.AddArray("eligibilities", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, proof := range b.EligibilityProofs {
			encoder.AppendObject(&proof)
		}
		return nil
	}))
	encoder.AddString("ref_ballot", b.RefBallot.String())
	encoder.AddInt("active_set_size", activeSetSize)
	encoder.AddString("beacon", beacon.ShortString())
	encoder.AddObject("votes", &b.Votes)
	return nil
}

// ToBallotIDs turns a list of Ballot into a list of BallotID.
func ToBallotIDs(ballots []*Ballot) []BallotID {
	ids := make([]BallotID, 0, len(ballots))
	for _, b := range ballots {
		ids = append(ids, b.ID())
	}
	return ids
}

// String returns a short prefix of the hex representation of the ID.
func (id BallotID) String() string {
	return id.AsHash32().ShortString()
}

// Bytes returns the BallotID as a byte slice.
func (id BallotID) Bytes() []byte {
	return id.AsHash32().Bytes()
}

// AsHash32 returns a Hash32 whose first 20 bytes are the bytes of this BallotID, it is right-padded with zeros.
func (id BallotID) AsHash32() Hash32 {
	return Hash20(id).ToHash32()
}

// Field returns a log field. Implements the LoggableField interface.
func (id BallotID) Field() log.Field {
	return log.String("ballot_id", id.String())
}

// Compare returns true if other (the given BallotID) is less than this BallotID, by lexicographic comparison.
func (id BallotID) Compare(other BallotID) bool {
	return bytes.Compare(id.Bytes(), other.Bytes()) < 0
}

// BallotIDsToHashes turns a list of BallotID into their Hash32 representation.
func BallotIDsToHashes(ids []BallotID) []Hash32 {
	hashes := make([]Hash32, 0, len(ids))
	for _, id := range ids {
		hashes = append(hashes, id.AsHash32())
	}
	return hashes
}

// NewExistingBallot creates ballot from stored data.
func NewExistingBallot(id BallotID, sig []byte, pub []byte, inner InnerBallot) Ballot {
	return Ballot{
		ballotID:    id,
		Signature:   sig,
		smesherID:   signing.NewPublicKey(pub),
		InnerBallot: inner,
	}
}
