package oracle

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/require"
	"testing"
)

var atxID = types.AtxId{Hash: [32]byte{1, 3, 3, 7}}
var nodeID, vrfSigner = generateNodeIDAndSigner()
var validateVrf = BLS381.Verify2
var privateKey, publicKey = BLS381.GenKeyPair(BLS381.DefaultSeed())
var ed = signing.NewEdSigner()

func generateNodeIDAndSigner() (types.NodeId, Signer) {
	return types.NodeId{
		Key:          ed.PublicKey().String(),
		VRFPublicKey: publicKey,
	}, BLS381.NewBlsSigner(privateKey)
}

type mockActivationDB struct {
	activeSetSize       uint32
	atxPublicationLayer types.LayerID
	atxs                map[string]map[types.LayerID]types.AtxId
}

func (a *mockActivationDB) IsIdentityActive(edId string, layer types.LayerID) (*types.NodeId, bool, types.AtxId, error) {
	if idmap, ok := a.atxs[edId]; ok {
		if atxid, ok := idmap[layer]; ok {
			return &types.NodeId{edId, publicKey}, true, atxid, nil
		}
	}
	return &types.NodeId{edId, publicKey}, true, *types.EmptyAtxId, nil
}

func (a mockActivationDB) ActiveSetSize(epochId types.EpochId) (uint32, error) {
	return a.activeSetSize, nil
}

func (a mockActivationDB) GetIdentity(edId string) (types.NodeId, error) {
	return types.NodeId{Key: edId, VRFPublicKey: publicKey}, nil
}

func (a mockActivationDB) GetNodeAtxIds(node types.NodeId) ([]types.AtxId, error) {
	if node.Key != nodeID.Key {
		return []types.AtxId{}, nil
	}
	return []types.AtxId{atxID}, nil
}

func (a mockActivationDB) GetAtx(id types.AtxId) (*types.ActivationTxHeader, error) {
	if id == atxID {
		atxHeader := &types.ActivationTxHeader{
			NIPSTChallenge: types.NIPSTChallenge{PubLayerIdx: a.atxPublicationLayer}, ActiveSetSize: a.activeSetSize,
		}
		atxHeader.SetId(&id)
		return atxHeader, nil
	}
	return nil, errors.New("wrong atx id")
}

func TestBlockOracle(t *testing.T) {
	r := require.New(t)

	// Happy flow with small numbers that can be inspected manually
	testBlockOracleAndValidator(r, 5, 10, 20)
	// Big, realistic numbers
	//testBlockOracleAndValidator(r, 3000, 200, 4032)
	// More miners than blocks (ensure at least one block per activation)
	testBlockOracleAndValidator(r, 5, 2, 2)
}

func testBlockOracleAndValidator(r *require.Assertions, activeSetSize uint32, committeeSize int32, layersPerEpoch uint16) {
	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(0), atxs: map[string]map[types.LayerID]types.AtxId{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(uint32(committeeSize), layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider,
		validateVrf, lg.WithName("blkElgValidator"))
	numberOfEpochsToTest := 2
	counterValuesSeen := map[uint32]int{}
	for layer := layersPerEpoch * 2; layer < layersPerEpoch*uint16(numberOfEpochsToTest+2); layer++ {
		activationDB.atxPublicationLayer = types.LayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.LayerID(layer)
		_, proofs, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)

		for _, proof := range proofs {
			block := newBlockWithEligibility(layerID, nodeID, atxID, proof, activationDB)
			eligible, err := validator.BlockSignedAndEligible(block)
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

func TestBlockOracleInGenesisReturnsNoAtx(t *testing.T) {
	r := require.New(t)
	activeSetSize := uint32(5)
	committeeSize := int32(10)
	layersPerEpoch := uint16(20)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(0), atxs: map[string]map[types.LayerID]types.AtxId{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(uint32(committeeSize), layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	for layer := uint16(0); layer < layersPerEpoch; layer++ {
		activationDB.atxPublicationLayer = types.LayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.LayerID(layer)
		atxId, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		r.Equal(*types.EmptyAtxId, atxId)
	}
}

func TestBlockOracleEmptyActiveSet(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(0) // Nobody is active 😱
	committeeSize := int32(200)
	layersPerEpoch := uint16(10)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.AtxId{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(uint32(committeeSize), layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	_, proofs, err := blockOracle.BlockEligible(types.LayerID(layersPerEpoch * 2))
	r.EqualError(err, "empty active set not allowed")
	r.Nil(proofs)
}

func TestBlockOracleEmptyActiveSetValidation(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(0) // Nobody is active 😰
	committeeSize := int32(200)
	layersPerEpoch := uint16(10)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.AtxId{}}
	beaconProvider := &EpochBeaconProvider{}

	lg := log.NewDefault(nodeID.Key[:5])
	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider,
		validateVrf, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(types.LayerID(layersPerEpoch*2), nodeID, atxID, types.BlockEligibilityProof{}, activationDB)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.EqualError(err, "empty active set not allowed")
	r.False(eligible)
}

func TestBlockOracleNoActivationsForNode(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(20)
	committeeSize := int32(200)
	layersPerEpoch := uint16(10)
	_, publicKey := BLS381.GenKeyPair(BLS381.DefaultSeed())
	nodeID := types.NodeId{
		Key:          "other key",
		VRFPublicKey: publicKey,
	} // This guy has no activations 🧐

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.AtxId{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(uint32(committeeSize), layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	_, proofs, err := blockOracle.BlockEligible(types.LayerID(layersPerEpoch * 2))
	r.EqualError(err, "failed to get latest ATX: not in genesis (epoch 2) yet failed to get atx: no activations found")
	r.Nil(proofs)
}

func TestBlockOracleValidatorInvalidProof(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(1)
	committeeSize := int32(10)
	layersPerEpoch := uint16(20)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.AtxId{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(uint32(committeeSize), layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.LayerID(layersPerEpoch * 2)

	_, proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	proof.Sig[0] += 1 // Messing with the proof 😈

	validator := NewBlockEligibilityValidator(committeeSize, layersPerEpoch, activationDB, beaconProvider,
		validateVrf, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, nodeID, atxID, proof, activationDB)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.False(eligible)
	r.Nil(err)
}

func TestBlockOracleValidatorInvalidProof2(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(10)
	layersPerEpoch := uint16(1)
	minerActivationDB := &mockActivationDB{activeSetSize: 1, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.AtxId{}}
	// Use different active set size to get more blocks 🤫
	validatorActivationDB := &mockActivationDB{activeSetSize: 10, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.AtxId{}}

	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, minerActivationDB, beaconProvider, vrfSigner, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.LayerID(layersPerEpoch * 2)

	_, proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	for i := 1; proof.J == 0; i++ {
		proof = proofs[i]
	}

	validator := NewBlockEligibilityValidator(int32(committeeSize), layersPerEpoch, validatorActivationDB, beaconProvider,
		validateVrf, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, nodeID, atxID, proof, validatorActivationDB)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.False(eligible)
	r.EqualError(err, fmt.Sprintf("proof counter (%d) must be less than number of eligible blocks (1)", proof.J))
}

func TestBlockOracleValidatorInvalidProof3(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(1)
	committeeSize := uint32(10)
	layersPerEpoch := uint16(20)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.AtxId{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, layersPerEpoch, activationDB, beaconProvider, vrfSigner, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.LayerID(layersPerEpoch * 2)

	_, proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	for i := 1; proof.J == 0; i++ {
		proof = proofs[i]
	}

	validatorActivationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(0), atxs: activationDB.atxs}
	validator := NewBlockEligibilityValidator(int32(committeeSize), layersPerEpoch, validatorActivationDB, beaconProvider,
		validateVrf, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, nodeID, atxID, proof, activationDB)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.False(eligible)
	r.EqualError(err, "ATX target epoch (1) doesn't match block publication epoch (2)")
}

func newBlockWithEligibility(layerID types.LayerID, nodeID types.NodeId, atxID types.AtxId,
	proof types.BlockEligibilityProof, db *mockActivationDB) *types.Block {
	block := &types.Block{MiniBlock: types.MiniBlock{BlockHeader: types.BlockHeader{LayerIndex: layerID, ATXID: atxID, EligibilityProof: proof}}}
	block.Signature = ed.Sign(block.Bytes())
	if _, ok := db.atxs[ed.PublicKey().String()]; !ok {
		db.atxs[ed.PublicKey().String()] = map[types.LayerID]types.AtxId{}
	}
	db.atxs[ed.PublicKey().String()][layerID] = atxID
	return block
}
