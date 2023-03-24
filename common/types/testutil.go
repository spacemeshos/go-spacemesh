package types

import (
	"math/rand"
)

// RandomBytes generates random data in bytes for testing.
func RandomBytes(size int) []byte {
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
	var b [ATXIDSize]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return *EmptyATXID
	}
	return ATXID(b)
}

// RandomNodeID generates a random NodeID for testing.
func RandomNodeID() NodeID {
	var b [NodeIDSize]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return EmptyNodeID
	}
	return NodeID(b)
}

// RandomBallotID generates a random BallotID for testing.
func RandomBallotID() BallotID {
	var b [BallotIDSize]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return EmptyBallotID
	}
	return BallotID(b)
}

// RandomProposalID generates a random ProposalID for testing.
func RandomProposalID() ProposalID {
	var b [ProposalIDSize]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return ProposalID{}
	}
	return ProposalID(b)
}

// RandomBlockID generates a random ProposalID for testing.
func RandomBlockID() BlockID {
	return BlockID(RandomProposalID())
}

// RandomTransactionID generates a random TransactionID for testing.
func RandomTransactionID() TransactionID {
	var b [TransactionIDSize]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return TransactionID{}
	}
	return TransactionID(b)
}

// RandomBallot generates a Ballot with random content for testing.
func RandomBallot() *Ballot {
	return &Ballot{
		BallotMetadata: BallotMetadata{
			Layer: NewLayerID(10),
		},
		InnerBallot: InnerBallot{
			AtxID:     RandomATXID(),
			RefBallot: RandomBallotID(),
		},
		Votes: Votes{
			Base:    RandomBallotID(),
			Support: []Vote{{ID: RandomBlockID()}, {ID: RandomBlockID()}},
		},
	}
}
