package types

import (
	"math/rand"
	"time"
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

// RandomBlockID generates a random BlockID for testing.
func RandomBlockID() BlockID {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, BlockIDSize)
	_, err := rand.Read(b)
	if err != nil {
		return BlockID{}
	}
	return BlockID(CalcHash32(b).ToHash20())
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
			BaseBallot: RandomBallotID(),
			ForDiff:    []BlockID{RandomBlockID(), RandomBlockID()},
			RefBallot:  RandomBallotID(),
			LayerIndex: NewLayerID(10),
		},
	}
}
