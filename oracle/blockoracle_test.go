package oracle

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/block"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/stretchr/testify/require"
	"testing"
)

var atxID = block.AtxId{Hash: [32]byte{1, 3, 3, 7}}
var nodeID, vrfSigner = generateNodeIDAndSigner()

func generateNodeIDAndSigner() (block.NodeId, *crypto.VRFSigner) {
	publicKey, privateKey, _ := crypto.GenerateVRFKeys()
	return block.NodeId{
		Key:          "key",
		VRFPublicKey: publicKey,
	}, crypto.NewVRFSigner(privateKey)
}

type mockActivationDB struct {
	activeSetSize uint32
	layerIndex    block.LayerID
}

func (a mockActivationDB) GetNodeAtxIds(node block.NodeId) ([]block.AtxId, error) {
	if node.Key != nodeID.Key {
		return []block.AtxId{}, nil
	}
	return []block.AtxId{atxID}, nil
}

func (a mockActivationDB) GetAtx(id block.AtxId) (*block.ActivationTx, error) {
	if id == atxID {
		return &block.ActivationTx{ActivationTxHeader: block.ActivationTxHeader{ActiveSetSize: a.activeSetSize,
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
		activationDB.layerIndex = block.LayerID((layer / layersPerEpoch) * layersPerEpoch)
		layerID := block.LayerID(layer)
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
	eligible, err := validator.BlockEligible(0, nodeID, block.BlockEligibilityProof{}, atxID)
	r.EqualError(err, "empty active set not allowed")
	r.False(eligible)
}

func TestBlockOracleNoActivationsForNode(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(20)
	committeeSize := int32(200)
	layersPerEpoch := uint16(10)
	publicKey, privateKey, err := crypto.GenerateVRFKeys()
	r.NoError(err)
	nodeID := block.NodeId{
		Key:          "other key",
		VRFPublicKey: publicKey,
	} // This guy has no activations 🧐

	activationDB := &mockActivationDB{activeSetSize: activeSetSize}
	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := crypto.NewVRFSigner(privateKey)
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
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	layerID := block.LayerID(0)

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
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, minerActivationDB, beaconProvider, vrfSigner, nodeID)

	layerID := block.LayerID(0)

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
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	layerID := block.LayerID(20)

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
