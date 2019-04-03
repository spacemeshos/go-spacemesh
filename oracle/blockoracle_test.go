package oracle

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/stretchr/testify/require"
	"testing"
)

var atxID = mesh.AtxId{Hash: [32]byte{1, 3, 3, 7}}

type mockActivationDB struct{}

func (mockActivationDB) GetNodeAtxIds(node mesh.NodeId) ([]mesh.AtxId, error) {
	return []mesh.AtxId{atxID}, nil
}

func (mockActivationDB) GetAtx(id mesh.AtxId) (*mesh.ActivationTx, error) {
	if id == atxID {
		return &mesh.ActivationTx{ActivationTxHeader: mesh.ActivationTxHeader{ActiveSetSize: 5}}, nil
	}
	return nil, errors.New("wrong atx id")
}

func TestBlockOracle(t *testing.T) {
	r := require.New(t)

	activationDB := &mockActivationDB{}
	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := &crypto.VRFSigner{}
	nodeID := mesh.NodeId{}
	layersPerEpoch := uint16(20)
	committeeSize := int32(10)

	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	validator := &MinerBlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDB,
		beaconProvider: beaconProvider,
		validateVRF:    crypto.ValidateVRF,
	}

	counterValuesSeen := map[uint32]bool{}
	for layer := uint16(0); layer < layersPerEpoch; layer++ {
		layerID := mesh.LayerID(layer)
		proofs, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)

		for _, proof := range proofs {
			eligible, err := validator.BlockEligible(layerID, nodeID, proof)
			r.True(eligible)
			r.NoError(err)
			counterValuesSeen[proof.J] = true
		}
	}

	numberOfEligibleBlocks := 40 // committeeSize * layersPerEpoch / activeSetSize = 10 * 20 / 5
	for c := 0; c < numberOfEligibleBlocks; c++ {
		r.True(counterValuesSeen[uint32(c)], "counter value %d expected, but not received", c)
	}
	r.Len(counterValuesSeen, numberOfEligibleBlocks)
}

// TODO: test empty active set (currently panics)
