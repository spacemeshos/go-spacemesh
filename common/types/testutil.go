package types

import (
	"math/rand"
	"time"

	"github.com/spacemeshos/go-spacemesh/signing"
)

// RandomBytes generates random data in bytes for testing.
func RandomBytes(size int) []byte {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return nil
	}
	return b
}

// RandomHash generates random Hash32 for testing.
func RandomHash() Hash32 {
	var h Hash32
	h.SetBytes(RandomBytes(Hash32Length))
	return h
}

// RandomBeacon generates random beacon in bytes for testing.
func RandomBeacon() Beacon {
	return BytesToBeacon(RandomBytes(BeaconSize))
}

// RandomActiveSet generates a random set of ATXIDs of the specified size.
func RandomActiveSet(size int) []ATXID {
	ids := make([]ATXID, 0, size)
	for i := 0; i < size; i++ {
		ids = append(ids, RandomATXID())
	}
	return ids
}

// RandomTXSet generates a random set of TransactionID of the specified size.
func RandomTXSet(size int) []TransactionID {
	ids := make([]TransactionID, 0, size)
	for i := 0; i < size; i++ {
		ids = append(ids, RandomTransactionID())
	}
	return ids
}

// RandomATXID generates a random ATXID for testing.
func RandomATXID() ATXID {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, ATXIDSize)
	_, err := rand.Read(b)
	if err != nil {
		return *EmptyATXID
	}
	return ATXID(CalcHash32(b))
}

// RandomBallotID generates a random BallotID for testing.
func RandomBallotID() BallotID {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, BallotIDSize)
	_, err := rand.Read(b)
	if err != nil {
		return EmptyBallotID
	}
	return BallotID(CalcHash32(b).ToHash20())
}

// RandomProposalID generates a random ProposalID for testing.
func RandomProposalID() ProposalID {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, ProposalIDSize)
	_, err := rand.Read(b)
	if err != nil {
		return ProposalID{}
	}
	return ProposalID(CalcHash32(b).ToHash20())
}

// RandomBlockID generates a random ProposalID for testing.
func RandomBlockID() BlockID {
	return BlockID(RandomProposalID())
}

// RandomTransactionID generates a random TransactionID for testing.
func RandomTransactionID() TransactionID {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, TransactionIDSize)
	_, err := rand.Read(b)
	if err != nil {
		return TransactionID{}
	}
	return TransactionID(CalcHash32(b))
}

// RandomBallot generates a Ballot with random content for testing.
func RandomBallot() *Ballot {
	return &Ballot{
		InnerBallot: InnerBallot{
			AtxID:      RandomATXID(),
			RefBallot:  RandomBallotID(),
			LayerIndex: NewLayerID(10),
		},
		Votes: Votes{
			Base:    RandomBallotID(),
			Support: []BlockID{RandomBlockID(), RandomBlockID()},
		},
	}
}

// GenLayerBallot generates a Ballot with random content for testing.
func GenLayerBallot(layerID LayerID) *Ballot {
	b := RandomBallot()
	b.LayerIndex = layerID
	signer := signing.NewEdSigner()
	b.Signature = signer.Sign(b.SignedBytes())
	b.Initialize()
	return b
}

// GenLayerBlock returns a Block in the given layer with the given data.
func GenLayerBlock(layerID LayerID, txs []TransactionID) *Block {
	b := &Block{
		InnerBlock: InnerBlock{
			LayerIndex: layerID,
			TxIDs:      txs,
		},
	}
	b.Initialize()
	return b
}

// GenLayerProposal returns a Proposal in the given layer with the given data.
func GenLayerProposal(layerID LayerID, txs []TransactionID) *Proposal {
	p := &Proposal{
		InnerProposal: InnerProposal{
			Ballot: Ballot{
				InnerBallot: InnerBallot{
					AtxID:      RandomATXID(),
					LayerIndex: layerID,
					EpochData: &EpochData{
						ActiveSet: RandomActiveSet(10),
						Beacon:    RandomBeacon(),
					},
				},
			},
			TxIDs: txs,
		},
	}
	signer := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())
	p.Initialize()
	return p
}
