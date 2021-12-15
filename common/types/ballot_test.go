package types

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestBallot_IDSize(t *testing.T) {
	var id BallotID
	assert.Len(t, id.Bytes(), BallotIDSize)
}

func TestBallot_Initialize(t *testing.T) {
	b := Ballot{
		InnerBallot: InnerBallot{
			AtxID:      RandomATXID(),
			BaseBallot: RandomBallotID(),
			ForDiff:    []BlockID{RandomBlockID(), RandomBlockID()},
			RefBallot:  RandomBallotID(),
			LayerIndex: NewLayerID(10),
		},
	}
	signer := signing.NewEdSigner()
	b.Signature = signer.Sign(b.Bytes())
	assert.NoError(t, b.Initialize())
	assert.NotEqual(t, EmptyBallotID, b.ID())
	assert.Equal(t, signer.PublicKey(), b.SmesherID())

	err := b.Initialize()
	assert.EqualError(t, err, "ballot already initialized")
}

func TestBallot_Initialize_BadSignature(t *testing.T) {
	b := Ballot{
		InnerBallot: InnerBallot{
			AtxID:      RandomATXID(),
			BaseBallot: RandomBallotID(),
			ForDiff:    []BlockID{RandomBlockID(), RandomBlockID()},
			RefBallot:  RandomBallotID(),
			LayerIndex: NewLayerID(10),
		},
	}
	b.Signature = signing.NewEdSigner().Sign(b.Bytes())[1:]
	err := b.Initialize()
	assert.EqualError(t, err, "ballot extract key: ed25519: bad signature format")
}

func TestDBBallot(t *testing.T) {
	layer := NewLayerID(100)
	b := GenLayerBallot(layer)
	assert.Equal(t, layer, b.LayerIndex)
	assert.NotEqual(t, b.ID(), EmptyBallotID)
	assert.NotNil(t, b.SmesherID())
	dbb := &DBBallot{
		InnerBallot: b.InnerBallot,
		ID:          b.ID(),
		Signature:   b.Signature,
	}
	got := dbb.ToBallot()
	assert.NotEqual(t, b, got)
	assert.Equal(t, b.ID(), got.ID())
	assert.Nil(t, got.smesherID)
	assert.Equal(t, b.SmesherID(), got.SmesherID())
	assert.Equal(t, b, got)
}
