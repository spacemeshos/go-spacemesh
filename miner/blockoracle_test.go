package miner

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/require"
	"testing"
)

var atxID = types.ATXID([32]byte{1, 3, 3, 7})
var nodeID, vrfsgn = generateNodeIDAndSigner()
var validateVRF = BLS381.Verify2
var vrfPrivkey, vrfPubkey = BLS381.GenKeyPair(BLS381.DefaultSeed())
var edSigner = signing.NewEdSigner()
var activeSetAtxs = []types.ATXID{types.ATXID([32]byte{1, 3, 3, 7}), types.ATXID([32]byte{1, 3, 3, 7}),
	types.ATXID([32]byte{1, 3, 3, 7}), types.ATXID([32]byte{1, 3, 3, 7}), types.ATXID([32]byte{1, 3, 3, 7})}

func generateNodeIDAndSigner() (types.NodeID, vrfSigner) {
	return types.NodeID{
		Key:          edSigner.PublicKey().String(),
		VRFPublicKey: vrfPubkey,
	}, BLS381.NewBlsSigner(vrfPrivkey)
}

type mockActivationDB struct {
	activeSetSize       uint32
	atxPublicationLayer types.LayerID
	atxs                map[string]map[types.LayerID]types.ATXID
	activeSetAtxs       []types.ATXID
}

func (a mockActivationDB) GetEpochAtxs(epochID types.EpochID) (atxs []types.ATXID) {
	if a.activeSetAtxs == nil {
		return activeSetAtxs
	}
	return a.activeSetAtxs
}

func (a mockActivationDB) GetIdentity(edID string) (types.NodeID, error) {
	return types.NodeID{Key: edID, VRFPublicKey: vrfPubkey}, nil
}

func (a mockActivationDB) GetNodeAtxIDForEpoch(nID types.NodeID, targetEpoch types.EpochID) (types.ATXID, error) {
	if nID.Key != nodeID.Key || targetEpoch.IsGenesis() {
		return *types.EmptyATXID, errors.New("not found")
	}
	return atxID, nil
}

func (a mockActivationDB) GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	if id == atxID {
		atxHeader := &types.ActivationTxHeader{
			NIPSTChallenge: types.NIPSTChallenge{
				NodeID: types.NodeID{
					Key:          edSigner.PublicKey().String(),
					VRFPublicKey: vrfPubkey,
				},
				PubLayerID: a.atxPublicationLayer,
			},
			ActiveSetSize: a.activeSetSize,
		}
		atxHeader.SetID(&id)
		return atxHeader, nil
	}
	return nil, errors.New("wrong atx id")
}

func TestBlockOracle(t *testing.T) {
	r := require.New(t)

	// Happy flow with small numbers that can be inspected manually
	types.SetLayersPerEpoch(20)
	testBlockOracleAndValidator(r, 5, 10, 20)
	// Big, realistic numbers
	// testBlockOracleAndValidator(r, 3000, 200, 4032)
	// More miners than blocks (ensure at least one block per activation)
	types.SetLayersPerEpoch(2)
	testBlockOracleAndValidator(r, 5, 2, 2)
}

func testBlockOracleAndValidator(r *require.Assertions, activeSetSize uint32, committeeSize uint32, layersPerEpoch uint16) {
	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(0), atxs: map[string]map[types.LayerID]types.ATXID{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, activeSetSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	validator := NewBlockEligibilityValidator(committeeSize, activeSetSize, layersPerEpoch, activationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	numberOfEpochsToTest := 2
	counterValuesSeen := map[uint32]int{}
	for layer := layersPerEpoch * 2; layer < layersPerEpoch*uint16(numberOfEpochsToTest+2); layer++ {
		activationDB.atxPublicationLayer = types.LayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.LayerID(layer)
		_, proofs, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)

		for _, proof := range proofs {
			block := newBlockWithEligibility(layerID, atxID, proof, activationDB, activeSetSize)
			eligible, err := validator.BlockSignedAndEligible(block)
			r.NoError(err, "at layer %d, with layersPerEpoch %d", layer, layersPerEpoch)
			r.True(eligible, "should be eligible at layer %d, but isn't", layer)
			counterValuesSeen[proof.J]++
		}
	}

	numberOfEligibleBlocks := committeeSize * uint32(layersPerEpoch) / activeSetSize
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
	committeeSize := uint32(10)
	layersPerEpoch := uint16(20)
	types.SetLayersPerEpoch(int32(layersPerEpoch))

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(0), atxs: map[string]map[types.LayerID]types.ATXID{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, activeSetSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	for layer := uint16(0); layer < layersPerEpoch; layer++ {
		activationDB.atxPublicationLayer = types.LayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.LayerID(layer)
		atxID, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		r.Equal(*types.EmptyATXID, atxID)
	}
}

func TestBlockOracleEmptyActiveSet(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(0) // Nobody is active 😱
	committeeSize := uint32(200)
	layersPerEpoch := uint16(10)

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}, activeSetAtxs: []types.ATXID{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, activeSetSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	_, proofs, err := blockOracle.BlockEligible(types.LayerID(layersPerEpoch * 2))
	r.EqualError(err, "empty active set not allowed")
	r.Nil(proofs)
}

func TestBlockOracleEmptyActiveSetValidation(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(0) // Nobody is active 😰
	committeeSize := uint32(200)
	layersPerEpoch := uint16(10)
	types.SetLayersPerEpoch(int32(layersPerEpoch))

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}}
	beaconProvider := &EpochBeaconProvider{}

	lg := log.NewDefault(nodeID.Key[:5])
	validator := NewBlockEligibilityValidator(committeeSize, activeSetSize, layersPerEpoch, activationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(types.LayerID(layersPerEpoch*2), atxID, types.BlockEligibilityProof{}, activationDB, 0)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.EqualError(err, "failed to get number of eligible blocks: empty active set not allowed")
	r.False(eligible)
}

func TestBlockOracleNoActivationsForNode(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(20)
	committeeSize := uint32(200)
	layersPerEpoch := uint16(10)
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	_, publicKey := BLS381.GenKeyPair(BLS381.DefaultSeed())
	nID := types.NodeID{
		Key:          "other key",
		VRFPublicKey: publicKey,
	} // This guy has no activations 🧐

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, activeSetSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nID, func() bool { return true }, lg.WithName("blockOracle"))

	_, proofs, err := blockOracle.BlockEligible(types.LayerID(layersPerEpoch * 2))
	r.EqualError(err, "failed to get latest ATX: failed to get ATX ID for target epoch 2: not found")
	r.Nil(proofs)
}

func TestBlockOracleValidatorInvalidProof(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(1)
	committeeSize := uint32(10)
	layersPerEpoch := uint16(20)
	types.SetLayersPerEpoch(int32(layersPerEpoch))

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, activeSetSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.LayerID(layersPerEpoch * 2)

	_, proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	proof.Sig[0]++ // Messing with the proof 😈

	validator := NewBlockEligibilityValidator(committeeSize, activeSetSize, layersPerEpoch, activationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, atxID, proof, activationDB, activeSetSize)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.EqualError(err, "eligibility VRF validation failed")
	r.False(eligible)
}

func TestBlockOracleValidatorInvalidProof2(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(10)
	layersPerEpoch := uint16(1)
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	minerActivationDB := &mockActivationDB{activeSetSize: 1, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}}
	// Use different active set size to get more blocks 🤫
	validatorActivationDB := &mockActivationDB{activeSetSize: 10, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}}

	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, 5, layersPerEpoch, minerActivationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.LayerID(layersPerEpoch * 2)

	_, proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	for i := 1; proof.J == 0; i++ {
		proof = proofs[i]
	}

	validator := NewBlockEligibilityValidator(committeeSize, 5, layersPerEpoch, validatorActivationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, atxID, proof, validatorActivationDB, committeeSize)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.False(eligible)
	r.EqualError(err, fmt.Sprintf("proof counter (%d) must be less than number of eligible blocks (1)", proof.J))
}

func TestBlockOracleValidatorInvalidProof3(t *testing.T) {
	r := require.New(t)

	activeSetSize := uint32(1)
	committeeSize := uint32(10)
	layersPerEpoch := uint16(20)
	types.SetLayersPerEpoch(int32(layersPerEpoch))

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, activeSetSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.LayerID(layersPerEpoch * 2)

	_, proofs, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	for i := 1; proof.J == 0; i++ {
		proof = proofs[i]
	}

	validatorActivationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(0), atxs: activationDB.atxs}
	validator := NewBlockEligibilityValidator(committeeSize, activeSetSize, layersPerEpoch, validatorActivationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, atxID, proof, activationDB, activeSetSize)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.False(eligible)
	r.EqualError(err, "ATX target epoch (1) doesn't match block publication epoch (2)")
}

func newBlockWithEligibility(layerID types.LayerID, atxID types.ATXID, proof types.BlockEligibilityProof, db *mockActivationDB, activeSetSize uint32) *types.Block {

	block := &types.Block{MiniBlock: types.MiniBlock{BlockHeader: types.BlockHeader{LayerIndex: layerID, ATXID: atxID, EligibilityProof: proof}}}
	block.ActiveSet = &[]types.ATXID{}
	for i := 0; i < int(activeSetSize); i++ {
		atx := types.ATXID([32]byte{1, 3, 3, byte(i)})
		*block.ActiveSet = append(*block.ActiveSet, atx)
	}
	block.Signature = edSigner.Sign(block.Bytes())

	if _, ok := db.atxs[edSigner.PublicKey().String()]; !ok {
		db.atxs[edSigner.PublicKey().String()] = map[types.LayerID]types.ATXID{}
	}
	block.Initialize()
	db.atxs[edSigner.PublicKey().String()][layerID] = atxID
	return block
}

func TestBlockEligibility_calc(t *testing.T) {
	r := require.New(t)
	atxH := types.NewActivationTx(types.NIPSTChallenge{PubLayerID: 0}, types.Address{}, 1, nil, nil)
	atxH.ActiveSetSize = 10
	atxDb := &mockAtxDB{atxH: atxH.ActivationTxHeader}
	genSetSize := uint32(0)
	o := NewMinerBlockOracle(10, genSetSize, 1, atxDb, &EpochBeaconProvider{}, vrfsgn, nodeID, func() bool { return true }, log.NewDefault(t.Name()))
	err := o.calcEligibilityProofs(1)
	r.EqualError(err, "empty active set not allowed") // a hack to make sure we got genesis active set size on genesis
}

func TestMinerBlockOracle_GetEligibleLayers(t *testing.T) {
	r := require.New(t)
	activeSetSize := uint32(5)
	committeeSize := uint32(10)
	layersPerEpoch := uint16(20)
	types.SetLayersPerEpoch(int32(layersPerEpoch))

	activationDB := &mockActivationDB{activeSetSize: activeSetSize, atxPublicationLayer: types.LayerID(0), atxs: map[string]map[types.LayerID]types.ATXID{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, activeSetSize, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	numberOfEpochsToTest := 1 // this test supports only 1 epoch
	eligibleLayers := 0
	for layer := layersPerEpoch * 2; layer < layersPerEpoch*uint16(numberOfEpochsToTest+2); layer++ {
		activationDB.atxPublicationLayer = types.LayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.LayerID(layer)
		_, proofs, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		if len(proofs) > 0 {
			eligibleLayers++
		}
	}
	r.Equal(eligibleLayers, len(blockOracle.GetEligibleLayers()))

}
