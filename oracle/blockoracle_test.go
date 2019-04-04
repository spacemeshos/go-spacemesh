package oracle

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/stretchr/testify/require"
	"testing"
)

var atxID = mesh.AtxId{Hash: [32]byte{1, 3, 3, 7}}
var nodeID = mesh.NodeId{
	Key: "key",
	Vrf: "vrf",
}

type mockActivationDB struct {
	activeSetSize uint32
	layerIndex    mesh.LayerID
}

func (a mockActivationDB) GetNodeAtxIds(node mesh.NodeId) ([]mesh.AtxId, error) {
	if node != nodeID {
		return []mesh.AtxId{}, nil
	}
	return []mesh.AtxId{atxID}, nil
}

func (a mockActivationDB) GetAtx(id mesh.AtxId) (*mesh.ActivationTx, error) {
	if id == atxID {
		return &mesh.ActivationTx{ActivationTxHeader: mesh.ActivationTxHeader{ActiveSetSize: a.activeSetSize,
			LayerIndex: a.layerIndex}}, nil
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
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)
	validator := &MinerBlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDB,
		beaconProvider: beaconProvider,
		validateVRF:    crypto.ValidateVRF,
	}
	numberOfEpochsToTest := 2
	counterValuesSeen := map[uint32]int{}
	for layer := uint16(0); layer < layersPerEpoch*uint16(numberOfEpochsToTest); layer++ {
		activationDB.layerIndex = mesh.LayerID((layer / layersPerEpoch) * layersPerEpoch)
		layerID := mesh.LayerID(layer)
		proofs, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)

		for _, proof := range proofs {
			eligible, err := validator.BlockEligible(layerID, nodeID, proof, atxID)
			r.NoError(err, "at layer %d, with layersPerEpoch %d", layer, layersPerEpoch)
			r.True(eligible, "should be eligible at layer %d, but isn't", layer)
			counterValuesSeen[proof.J] += 1
		}
	}
	numberOfEligibleBlocks := uint32(committeeSize) * uint32(layersPerEpoch) / activeSetSize
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}
	for c := uint32(0); c < numberOfEligibleBlocks; c++ {
		r.Equal(numberOfEpochsToTest, counterValuesSeen[c],
			"counter value %d expected %d times, but received %d times",
			c, numberOfEpochsToTest, counterValuesSeen[c])
	}
	r.Len(counterValuesSeen, int(numberOfEligibleBlocks))
}

func TestBlockOracleEmptyActiveSet(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(0) // Nobody is active 😱
	committeeSize := int32(200)
	layersPerEpoch := uint16(10)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize}
	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := &crypto.VRFSigner{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	proofs, err := blockOracle.BlockEligible(0)
	r.EqualError(err, "empty active set not allowed")
	r.Nil(proofs)
}

func TestBlockOracleEmptyActiveSetValidation(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(0) // Nobody is active 😰
	committeeSize := int32(200)
	layersPerEpoch := uint16(10)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize}
	beaconProvider := &EpochBeaconProvider{}

	validator := &MinerBlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDB,
		beaconProvider: beaconProvider,
		validateVRF:    crypto.ValidateVRF,
	}
	eligible, err := validator.BlockEligible(0, nodeID, mesh.BlockEligibilityProof{}, atxID)
	r.EqualError(err, "empty active set not allowed")
	r.False(eligible)
}

func TestBlockOracleNoActivationsForNode(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(20)
	committeeSize := int32(200)
	layersPerEpoch := uint16(10)
	nodeID := mesh.NodeId{
		Key: "other key",
		Vrf: "other vrf",
	} // This guy has no activations 🧐

	activationDB := &mockActivationDB{activeSetSize: activeSetSize}
	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := &crypto.VRFSigner{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	proofs, err := blockOracle.BlockEligible(0)
	r.EqualError(err, "no activations found")
	r.Nil(proofs)
}

func TestBlockOracleValidatorInvalidProof(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(5)
	committeeSize := int32(10)
	layersPerEpoch := uint16(20)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize}
	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := &crypto.VRFSigner{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	layerID := mesh.LayerID(0)

	proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	proof.J += 1 // Messing with the proof 😈

	validator := &MinerBlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDB,
		beaconProvider: beaconProvider,
		validateVRF:    crypto.ValidateVRF,
	}
	eligible, err := validator.BlockEligible(layerID, nodeID, proof, atxID)
	r.False(eligible)
	r.EqualError(err, "VRF validation failed")
}

func TestBlockOracleValidatorInvalidProof2(t *testing.T) {
	r := require.New(t)

	committeeSize := int32(10)
	layersPerEpoch := uint16(1)
	minerActivationDB := &mockActivationDB{activeSetSize: 1}
	validatorActivationDB := &mockActivationDB{activeSetSize: 10} // Use different active set size to get more blocks 🤫

	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := &crypto.VRFSigner{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, minerActivationDB, beaconProvider, vrfSigner, nodeID)

	layerID := mesh.LayerID(0)

	proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	for i := 1; proof.J == 0; i++ {
		proof = proofs[i]
	}

	validator := &MinerBlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   validatorActivationDB,
		beaconProvider: beaconProvider,
		validateVRF:    crypto.ValidateVRF,
	}
	eligible, err := validator.BlockEligible(layerID, nodeID, proof, atxID)
	r.False(eligible)
	r.EqualError(err, fmt.Sprintf("proof counter (%d) must be less than number of eligible blocks (1)", proof.J))
}

func TestBlockOracleValidatorInvalidProof3(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(1)
	committeeSize := int32(10)
	layersPerEpoch := uint16(20)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize}
	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := &crypto.VRFSigner{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	layerID := mesh.LayerID(20)

	proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	for i := 1; proof.J == 0; i++ {
		proof = proofs[i]
	}

	validator := &MinerBlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDB,
		beaconProvider: beaconProvider,
		validateVRF:    crypto.ValidateVRF,
	}
	eligible, err := validator.BlockEligible(layerID, nodeID, proof, atxID)
	r.False(eligible)
	r.EqualError(err, "activation epoch (0) mismatch with layer epoch (1)")
}
