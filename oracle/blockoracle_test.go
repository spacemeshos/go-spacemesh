package oracle

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/require"
	"testing"
)

var atxID = types.AtxId{Hash: [32]byte{1, 3, 3, 7}}
var nodeID, vrfSigner = generateNodeIDAndSigner()

func generateNodeIDAndSigner() (types.NodeId, *crypto.VRFSigner) {
	publicKey, privateKey, _ := crypto.GenerateVRFKeys()
	return types.NodeId{
		Key:          "edKey",
		VRFPublicKey: publicKey,
	}, crypto.NewVRFSigner(privateKey)
}

type mockActivationDB struct {
	activeSetSize uint32
	layerIndex    types.LayerID
}

func (a mockActivationDB) GetNodeAtxIds(node types.NodeId) ([]types.AtxId, error) {
	if node.Key != nodeID.Key {
		return []types.AtxId{}, nil
	}
	return []types.AtxId{atxID}, nil
}

func (a mockActivationDB) GetAtx(id types.AtxId) (*types.ActivationTx, error) {
	if id == atxID {
		return &types.ActivationTx{ActivationTxHeader: types.ActivationTxHeader{
				NIPSTChallenge: types.NIPSTChallenge{LayerIdx: a.layerIndex}, ActiveSetSize: a.activeSetSize, Valid: true}},
			nil
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
	activationDB := &mockActivationDB{activeSetSize: activeSetSize, layerIndex: types.LayerID(layersPerEpoch)}
	beaconProvider := &EpochBeaconProvider{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)
	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider,
		crypto.ValidateVRF)
	numberOfEpochsToTest := 2
	counterValuesSeen := map[uint32]int{}
	for layer := layersPerEpoch * 1; layer < layersPerEpoch*uint16(numberOfEpochsToTest+1); layer++ {
		activationDB.layerIndex = types.LayerID((layer / layersPerEpoch) * layersPerEpoch)
		layerID := types.LayerID(layer)
		_, proofs, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)

		for _, proof := range proofs {
			block := newBlockWithEligibility(layerID, nodeID, atxID, proof)
			eligible, err := validator.BlockEligible(block)
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

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, layerIndex: types.LayerID(layersPerEpoch)}
	beaconProvider := &EpochBeaconProvider{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	_, proofs, err := blockOracle.BlockEligible(types.LayerID(layersPerEpoch))
	r.EqualError(err, "empty active set not allowed")
	r.Nil(proofs)
}

func TestBlockOracleEmptyActiveSetValidation(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(0) // Nobody is active 😰
	committeeSize := int32(200)
	layersPerEpoch := uint16(10)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, layerIndex: types.LayerID(layersPerEpoch)}
	beaconProvider := &EpochBeaconProvider{}

	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider,
		crypto.ValidateVRF)
	block := newBlockWithEligibility(types.LayerID(layersPerEpoch), nodeID, atxID, types.BlockEligibilityProof{})
	eligible, err := validator.BlockEligible(block)
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
	nodeID := types.NodeId{
		Key:          "other key",
		VRFPublicKey: publicKey,
	} // This guy has no activations 🧐

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, layerIndex: types.LayerID(layersPerEpoch)}
	beaconProvider := &EpochBeaconProvider{}
	vrfSigner := crypto.NewVRFSigner(privateKey)
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	_, proofs, err := blockOracle.BlockEligible(types.LayerID(layersPerEpoch))
	r.EqualError(err, "failed to calculate active set size: no activations found")
	r.Nil(proofs)
}

func TestBlockOracleValidatorInvalidProof(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(1)
	committeeSize := int32(10)
	layersPerEpoch := uint16(20)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, layerIndex: types.LayerID(layersPerEpoch)}
	beaconProvider := &EpochBeaconProvider{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	layerID := types.LayerID(layersPerEpoch)

	_, proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	proof.Sig[0] += 1 // Messing with the proof 😈

	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider,
		crypto.ValidateVRF)
	block := newBlockWithEligibility(layerID, nodeID, atxID, proof)
	eligible, err := validator.BlockEligible(block)
	r.False(eligible)
	r.EqualError(err, "VRF validation failed")
}

func TestBlockOracleValidatorInvalidProof2(t *testing.T) {
	r := require.New(t)

	committeeSize := int32(10)
	layersPerEpoch := uint16(1)
	minerActivationDB := &mockActivationDB{activeSetSize: 1, layerIndex: types.LayerID(layersPerEpoch)}
	// Use different active set size to get more blocks 🤫
	validatorActivationDB := &mockActivationDB{activeSetSize: 10, layerIndex: types.LayerID(layersPerEpoch)}

	beaconProvider := &EpochBeaconProvider{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, minerActivationDB, beaconProvider, vrfSigner, nodeID)

	layerID := types.LayerID(layersPerEpoch)

	_, proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	for i := 1; proof.J == 0; i++ {
		proof = proofs[i]
	}

	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, validatorActivationDB, beaconProvider,
		crypto.ValidateVRF)
	block := newBlockWithEligibility(layerID, nodeID, atxID, proof)
	eligible, err := validator.BlockEligible(block)
	r.False(eligible)
	r.EqualError(err, fmt.Sprintf("proof counter (%d) must be less than number of eligible blocks (1)", proof.J))
}

func TestBlockOracleValidatorInvalidProof3(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(1)
	committeeSize := int32(10)
	layersPerEpoch := uint16(20)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, layerIndex: types.LayerID(layersPerEpoch)}
	beaconProvider := &EpochBeaconProvider{}
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID)

	layerID := types.LayerID(layersPerEpoch * 2)

	_, proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	for i := 1; proof.J == 0; i++ {
		proof = proofs[i]
	}

	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider,
		crypto.ValidateVRF)
	block := newBlockWithEligibility(layerID, nodeID, atxID, proof)
	eligible, err := validator.BlockEligible(block)
	r.False(eligible)
	r.EqualError(err, "activation epoch (1) mismatch with layer epoch (2)")
}

func newBlockWithEligibility(layerID types.LayerID, nodeID types.NodeId, atxID types.AtxId,
	proof types.BlockEligibilityProof) *types.Block {

	return &types.Block{BlockHeader: types.BlockHeader{LayerIndex: layerID, MinerID: nodeID, ATXID: atxID,
		EligibilityProof: proof}}
}
