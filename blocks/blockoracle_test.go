package blocks

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

var atxID = types.ATXID([32]byte{1, 3, 3, 7})
var nodeID, vrfsgn = generateNodeIDAndSigner()
var validateVRF = signing.VRFVerify
var edSigner = signing.NewEdSigner()
var activeSetAtxs = []types.ATXID{atxID, atxID, atxID, atxID, atxID, atxID, atxID, atxID, atxID, atxID} // 10 ATXs

const defaultAtxWeight = 1024

func generateNodeIDAndSigner() (types.NodeID, vrfSigner) {
	edPubkey := edSigner.PublicKey()
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(edPubkey.Bytes())
	if err != nil {
		panic("failed to create vrf signer")
	}
	return types.NodeID{
		Key:          edPubkey.String(),
		VRFPublicKey: vrfPubkey,
	}, vrfSigner
}

type mockActivationDB struct {
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
	return types.NodeID{Key: edID, VRFPublicKey: nodeID.VRFPublicKey}, nil
}

func (a mockActivationDB) GetNodeAtxIDForEpoch(nID types.NodeID, targetEpoch types.EpochID) (types.ATXID, error) {
	if nID.Key != nodeID.Key || targetEpoch == 0 {
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
					VRFPublicKey: nodeID.VRFPublicKey,
				},
				PubLayerID: a.atxPublicationLayer,
				StartTick:  0,
				EndTick:    1,
			},
			Space: defaultAtxWeight,
		}
		atxHeader.SetID(&id)
		return atxHeader, nil
	}
	return nil, errors.New("wrong atx id")
}

func (a mockActivationDB) GetEpochWeight(epochID types.EpochID) (uint64, []types.ATXID, error) {
	return uint64(len(a.GetEpochAtxs(epochID-1))) * defaultAtxWeight, nil, nil
}

func TestBlockOracle(t *testing.T) {
	r := require.New(t)

	// Happy flow with small numbers that can be inspected manually
	testBlockOracleAndValidator(r, 10*defaultAtxWeight, 10, 20)

	// Big, realistic numbers
	// testBlockOracleAndValidator(r, 3000*defaultAtxWeight, 200, 4032) // commented out because it takes VERY long

	// More miners than blocks (ensure at least one block per activation)
	testBlockOracleAndValidator(r, 10*defaultAtxWeight, 2, 2)
}

func testBlockOracleAndValidator(r *require.Assertions, totalWeight uint64, committeeSize uint32, layersPerEpoch uint16) {
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	activationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(0)}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, totalWeight, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	validator := NewBlockEligibilityValidator(committeeSize, totalWeight, layersPerEpoch, activationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	numberOfEpochsToTest := 2
	counterValuesSeen := map[uint32]int{}
	for layer := layersPerEpoch * 2; layer < layersPerEpoch*uint16(numberOfEpochsToTest+2); layer++ {
		activationDB.atxPublicationLayer = types.LayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.LayerID(layer)
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)

		for _, proof := range proofs {
			block := newBlockWithEligibility(layerID, atxID, proof, activationDB)
			eligible, err := validator.BlockSignedAndEligible(block)
			r.NoError(err, "at layer %d, with layersPerEpoch %d", layer, layersPerEpoch)
			r.True(eligible, "should be eligible at layer %d, but isn't", layer)
			counterValuesSeen[proof.J]++
		}
	}

	numberOfEligibleBlocks := committeeSize * uint32(layersPerEpoch) * defaultAtxWeight / uint32(totalWeight)
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
	totalWeight := uint64(5 * 131072)
	committeeSize := uint32(10)
	layersPerEpoch := uint16(20)
	types.SetLayersPerEpoch(int32(layersPerEpoch))

	activationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(0)}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, totalWeight, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	for layer := uint16(0); layer < layersPerEpoch; layer++ {
		activationDB.atxPublicationLayer = types.LayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.LayerID(layer)
		atxID, _, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		r.Equal(*types.EmptyATXID, atxID)
	}
}

func TestBlockOracleEmptyActiveSet(t *testing.T) {
	types.SetLayersPerEpoch(3)
	r := require.New(t)

	totalWeight := uint64(0) // Nobody is active 😱
	committeeSize := uint32(200)
	layersPerEpoch := uint16(10)

	activationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(layersPerEpoch), activeSetAtxs: []types.ATXID{}}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, totalWeight, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	_, proofs, _, err := blockOracle.BlockEligible(types.LayerID(layersPerEpoch * 3))
	r.EqualError(err, "zero total weight not allowed")
	r.Nil(proofs)
}

func TestBlockOracleEmptyActiveSetValidation(t *testing.T) {
	r := require.New(t)

	totalWeight := uint64(0) // Nobody is active 😰
	committeeSize := uint32(200)
	layersPerEpoch := uint16(10)
	types.SetLayersPerEpoch(int32(layersPerEpoch))

	activationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(layersPerEpoch)}
	beaconProvider := &EpochBeaconProvider{}

	lg := log.NewDefault(nodeID.Key[:5])
	validator := NewBlockEligibilityValidator(committeeSize, totalWeight, layersPerEpoch, activationDB, beaconProvider,
		validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(types.LayerID(layersPerEpoch*2), atxID, types.BlockEligibilityProof{}, activationDB)
	block.ActiveSet = &[]types.ATXID{}
	eligible, err := validator.BlockSignedAndEligible(block)
	r.EqualError(err, "failed to get number of eligible blocks: zero total weight not allowed")
	r.False(eligible)
}

func TestBlockOracleNoActivationsForNode(t *testing.T) {
	r := require.New(t)

	totalWeight := uint64(20)
	committeeSize := uint32(200)
	layersPerEpoch := uint16(10)
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	seed := make([]byte, 32)
	rand.Read(seed)
	_, publicKey, err := signing.NewVRFSigner(seed)
	r.NoError(err)
	nID := types.NodeID{
		Key:          "other key",
		VRFPublicKey: publicKey,
	} // This guy has no activations 🧐

	activationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(layersPerEpoch)}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, totalWeight, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nID, func() bool { return true }, lg.WithName("blockOracle"))

	_, proofs, _, err := blockOracle.BlockEligible(types.LayerID(layersPerEpoch * 2))
	r.EqualError(err, "failed to get latest atx for node in epoch 2: failed to get atx id for target epoch 2: not found")
	r.Nil(proofs)
}

func TestBlockOracleValidatorInvalidProof(t *testing.T) {
	r := require.New(t)

	totalWeight := uint64(1)
	committeeSize := uint32(10)
	layersPerEpoch := uint16(20)
	types.SetLayersPerEpoch(int32(layersPerEpoch))

	activationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(layersPerEpoch)}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, totalWeight, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.LayerID(layersPerEpoch * 2)

	var proof types.BlockEligibilityProof
	for ; ; layerID++ {
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		if len(proofs) > 0 {
			proof = proofs[0]
			break
		}
	}
	proof.Sig[0]++ // Messing with the proof 😈

	validator := NewBlockEligibilityValidator(committeeSize, totalWeight, layersPerEpoch, activationDB, beaconProvider,
		validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, atxID, proof, activationDB)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.EqualError(err, "eligibility VRF validation failed")
	r.False(eligible)
}

func TestBlockOracleValidatorInvalidProof2(t *testing.T) {
	r := require.New(t)

	committeeSize := uint32(10)
	layersPerEpoch := uint16(1)
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	minerActivationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(layersPerEpoch), activeSetAtxs: activeSetAtxs[:1]}
	// minerActivationDB := &mockActivationDB{totalWeight: 1 * defaultAtxWeight, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}}
	// Use different active set size to get more blocks 🤫
	validatorActivationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(layersPerEpoch), activeSetAtxs: activeSetAtxs}
	// validatorActivationDB := &mockActivationDB{totalWeight: 10 * defaultAtxWeight, atxPublicationLayer: types.LayerID(layersPerEpoch), atxs: map[string]map[types.LayerID]types.ATXID{}}

	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, 1*defaultAtxWeight, layersPerEpoch, minerActivationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.LayerID(layersPerEpoch * 2)

	_, proofs, _, err := blockOracle.BlockEligible(layerID)
	r.NoError(err)
	r.NotNil(proofs)

	proof := proofs[0]
	for i := 1; proof.J == 0; i++ {
		proof = proofs[i]
	}

	validator := NewBlockEligibilityValidator(committeeSize, 1*defaultAtxWeight, layersPerEpoch, validatorActivationDB,
		beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, atxID, proof, validatorActivationDB)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.False(eligible)
	r.EqualError(err, fmt.Sprintf("proof counter (%d) must be less than number of eligible blocks (1), totalWeight (10240)", proof.J))
}

func TestBlockOracleValidatorInvalidProof3(t *testing.T) {
	r := require.New(t)

	totalWeight := uint64(1)
	committeeSize := uint32(10)
	layersPerEpoch := uint16(20)
	types.SetLayersPerEpoch(int32(layersPerEpoch))

	activationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(layersPerEpoch)}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, totalWeight, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))

	layerID := types.LayerID(layersPerEpoch * 2)

	var proof types.BlockEligibilityProof
	for ; ; layerID++ {
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		// we want a proof with J != 0, so we must have more than one proof in the list
		if len(proofs) > 1 {
			proof = proofs[0]
			// we keep trying until J != 0
			for i := 1; proof.J == 0; i++ {
				proof = proofs[i]
			}
			break
		}
	}

	validatorActivationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(0), atxs: activationDB.atxs}
	validator := NewBlockEligibilityValidator(committeeSize, totalWeight, layersPerEpoch, validatorActivationDB, beaconProvider, validateVRF, nil, lg.WithName("blkElgValidator"))
	block := newBlockWithEligibility(layerID, atxID, proof, activationDB)
	eligible, err := validator.BlockSignedAndEligible(block)
	r.False(eligible)
	r.EqualError(err, "ATX target epoch (1) doesn't match block publication epoch (2)")
}

func newBlockWithEligibility(layerID types.LayerID, atxID types.ATXID, proof types.BlockEligibilityProof,
	db *mockActivationDB) *types.Block {

	block := &types.Block{MiniBlock: types.MiniBlock{BlockHeader: types.BlockHeader{
		LayerIndex:       layerID,
		ATXID:            atxID,
		EligibilityProof: proof,
	}}}
	epochAtxs := db.GetEpochAtxs(layerID.GetEpoch())
	block.ActiveSet = &epochAtxs
	block.Signature = edSigner.Sign(block.Bytes())
	if db.atxs == nil {
		db.atxs = map[string]map[types.LayerID]types.ATXID{}
	}
	if _, ok := db.atxs[edSigner.PublicKey().String()]; !ok {
		db.atxs[edSigner.PublicKey().String()] = map[types.LayerID]types.ATXID{}
	}
	block.Initialize()
	db.atxs[edSigner.PublicKey().String()][layerID] = atxID
	return block
}

func TestBlockEligibility_calc(t *testing.T) {
	r := require.New(t)
	atxH := types.NewActivationTx(types.NIPSTChallenge{PubLayerID: 0}, types.Address{}, nil, 0, nil)
	atxDb := &mockAtxDB{atxH: atxH.ActivationTxHeader}
	o := NewMinerBlockOracle(10, 0, 1, atxDb, &EpochBeaconProvider{}, vrfsgn, nodeID, func() bool { return true }, log.NewDefault(t.Name()))
	_, err := o.calcEligibilityProofs(1)
	r.EqualError(err, "zero total weight not allowed") // a hack to make sure we got genesis active set size on genesis
}

func TestMinerBlockOracle_GetEligibleLayers(t *testing.T) {
	r := require.New(t)
	totalWeight := uint64(5)
	committeeSize := uint32(10)
	layersPerEpoch := uint16(20)
	types.SetLayersPerEpoch(int32(layersPerEpoch))

	activationDB := &mockActivationDB{atxPublicationLayer: types.LayerID(0)}
	beaconProvider := &EpochBeaconProvider{}
	lg := log.NewDefault(nodeID.Key[:5])
	blockOracle := NewMinerBlockOracle(committeeSize, totalWeight, layersPerEpoch, activationDB, beaconProvider, vrfsgn, nodeID, func() bool { return true }, lg.WithName("blockOracle"))
	numberOfEpochsToTest := 1 // this test supports only 1 epoch
	eligibleLayers := 0
	for layer := layersPerEpoch * 2; layer < layersPerEpoch*uint16(numberOfEpochsToTest+2); layer++ {
		activationDB.atxPublicationLayer = types.LayerID((layer/layersPerEpoch)*layersPerEpoch - 1)
		layerID := types.LayerID(layer)
		_, proofs, _, err := blockOracle.BlockEligible(layerID)
		r.NoError(err)
		if len(proofs) > 0 {
			eligibleLayers++
		}
	}
	r.Equal(eligibleLayers, len(blockOracle.GetEligibleLayers()))

}
