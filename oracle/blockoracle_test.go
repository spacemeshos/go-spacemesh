package oracle

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/stretchr/testify/require"
	"testing"
)

var atxID = mesh.AtxId{Hash: [32]byte{1, 3, 3, 7}}

type mockActivationDB struct {
	activeSetSize uint32
}

func (a mockActivationDB) GetNodeAtxIds(node mesh.NodeId) ([]mesh.AtxId, error) {
	return []mesh.AtxId{atxID}, nil
}

func (a mockActivationDB) GetAtx(id mesh.AtxId) (*mesh.ActivationTx, error) {
	if id == atxID {
		return &mesh.ActivationTx{ActivationTxHeader: mesh.ActivationTxHeader{ActiveSetSize: a.activeSetSize}}, nil
	}
	return nil, errors.New("wrong atx id")
}

func TestBlockOracle(t *testing.T) {
	r := require.New(t)

	// Happy flow with small numbers that can be inspected manually
	testBlockOracleAndValidator(r, 5, 10, 20)
	// Big, realistic numbers
	testBlockOracleAndValidator(r, 3000, 200, 4032)
	// More miners than blocks (ensure at least one block per activation)
	testBlockOracleAndValidator(r, 5, 2, 2)
}

func testBlockOracleAndValidator(r *require.Assertions, activeSetSize uint32, committeeSize int32, layersPerEpoch uint16) {
	activationDB := &mockActivationDB{activeSetSize: activeSetSize}
	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := &crypto.VRFSigner{}
	nodeID := mesh.NodeId{}
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
	numberOfEligibleBlocks := uint32(committeeSize) * uint32(layersPerEpoch) / activeSetSize
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}
	for c := uint32(0); c < numberOfEligibleBlocks; c++ {
		r.True(counterValuesSeen[c], "counter value %d expected, but not received", c)
	}
	r.Len(counterValuesSeen, int(numberOfEligibleBlocks))
}

func TestBlockOracleEmptyActiveSet(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(0)
	committeeSize := int32(200)
	layersPerEpoch := uint16(10)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize}
	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := &crypto.VRFSigner{}
	nodeID := mesh.NodeId{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	proofs, err := blockOracle.BlockEligible(0)
	r.EqualError(err, "empty active set not allowed")
	r.Nil(proofs)
}

func TestBlockOracleEmptyActiveSetValidation(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(0)
	committeeSize := int32(200)
	layersPerEpoch := uint16(10)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize}
	beaconProvider := &EpochBeaconProvider{}
	nodeID := mesh.NodeId{}

	validator := &MinerBlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDB,
		beaconProvider: beaconProvider,
		validateVRF:    crypto.ValidateVRF,
	}
	eligible, err := validator.BlockEligible(0, nodeID, mesh.BlockEligibilityProof{})
	r.EqualError(err, "empty active set not allowed")
	r.False(eligible)
}

func TestBlockOracleValidatorInvalidProof(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(5)
	committeeSize := int32(10)
	layersPerEpoch := uint16(20)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize}
	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := &crypto.VRFSigner{}
	nodeID := mesh.NodeId{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	layerID := mesh.LayerID(0)

	proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	proof.J += 1

	validator := &MinerBlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDB,
		beaconProvider: beaconProvider,
		validateVRF:    crypto.ValidateVRF,
	}
	eligible, err := validator.BlockEligible(layerID, nodeID, proof)
	r.False(eligible)
	r.EqualError(err, "VRF validation failed")
}
