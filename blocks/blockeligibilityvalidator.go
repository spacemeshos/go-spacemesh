package blocks

import (
	"fmt"

	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// VRFValidationFunction is the VRF validation function.
type VRFValidationFunction func(message, signature, publicKey []byte) (bool, error)

type blockDB interface {
	GetBlock(ID types.BlockID) (*types.Block, error)
}

// BlockEligibilityValidator holds all the dependencies for validating block eligibility.
type BlockEligibilityValidator struct {
	committeeSize        uint32
	genesisActiveSetSize uint32
	layersPerEpoch       uint16
	activationDb         activationDB
	blocks               blockDB
	beaconProvider       *EpochBeaconProvider
	validateVRF          VRFValidationFunction
	log                  log.Log
}

// NewBlockEligibilityValidator returns a new BlockEligibilityValidator.
func NewBlockEligibilityValidator(committeeSize, genesisActiveSetSize uint32, layersPerEpoch uint16, activationDb activationDB, beaconProvider *EpochBeaconProvider, validateVRF VRFValidationFunction, blockDB blockDB, log log.Log) *BlockEligibilityValidator {

	return &BlockEligibilityValidator{
		committeeSize:        committeeSize,
		genesisActiveSetSize: genesisActiveSetSize,
		layersPerEpoch:       layersPerEpoch,
		activationDb:         activationDb,
		beaconProvider:       beaconProvider,
		validateVRF:          validateVRF,
		blocks:               blockDB,
		log:                  log,
	}
}

// BlockSignedAndEligible checks that a given block is signed and eligible. It returns true with no error or false and
// an error that explains why validation failed.
func (v BlockEligibilityValidator) BlockSignedAndEligible(block *types.Block) (bool, error) {
	epochNumber := block.LayerIndex.GetEpoch()
	if epochNumber == 0 {
		v.log.With().Warning("skipping epoch 0 block validation",
			block.ID(), block.LayerIndex)
		return true, nil
	}
	var err error
	activeSetBlock := block
	if block.RefBlock != nil {
		activeSetBlock, err = v.blocks.GetBlock(*block.RefBlock)
		if err != nil {
			//block should be present because we've synced it in the calling function
			return false, fmt.Errorf("cannot get refrence block %v", *block.RefBlock)
		}

	}
	if activeSetBlock.ActiveSet == nil {
		return false, fmt.Errorf("cannot get active set from block %v", activeSetBlock.ID())
	}
	//todo: optimise by using reference to active set size and cache active set size to not load all atxsIDs from db
	activeSetSize := uint32(len(*activeSetBlock.ActiveSet))
	atx, err := v.getValidAtx(block)
	if err != nil {
		return false, err
	}
	vrfPubkey := atx.NodeID.VRFPublicKey

	if epochNumber.IsGenesis() {
		v.log.With().Info("using genesisActiveSetSize",
			block.ID(), log.Uint32("genesisActiveSetSize", v.genesisActiveSetSize))
		activeSetSize = v.genesisActiveSetSize
	}

	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(activeSetSize, v.committeeSize, v.layersPerEpoch)
	if err != nil {
		return false, fmt.Errorf("failed to get number of eligible blocks: %v", err)
	}

	counter := block.EligibilityProof.J
	if counter >= numberOfEligibleBlocks {
		return false, fmt.Errorf("proof counter (%d) must be less than number of eligible blocks (%d), activeset %v", counter,
			numberOfEligibleBlocks, activeSetSize)
	}

	epochBeacon := v.beaconProvider.GetBeacon(epochNumber)
	message := serializeVRFMessage(epochBeacon, epochNumber, counter)
	vrfSig := block.EligibilityProof.Sig

	res, err := v.validateVRF(message, vrfSig, vrfPubkey)
	if err != nil {
		return false, fmt.Errorf("eligibility VRF validation failed: %v", err)
	}

	if !res {
		return false, fmt.Errorf("eligibility VRF validation failed")
	}

	eligibleLayer := calcEligibleLayer(epochNumber, v.layersPerEpoch, sha256.Sum256(vrfSig))

	if block.LayerIndex != eligibleLayer {
		return false, fmt.Errorf("block layer (%v) does not match eligibility layer (%v)",
			block.LayerIndex, eligibleLayer)
	}
	return true, nil
}

func (v BlockEligibilityValidator) getValidAtx(block *types.Block) (*types.ActivationTxHeader, error) {
	blockEpoch := block.LayerIndex.GetEpoch()
	atx, err := v.activationDb.GetAtxHeader(block.ATXID)
	if err != nil {
		return nil, fmt.Errorf("getting ATX failed: %v %v ep(%v)", err, block.ATXID.ShortString(), blockEpoch)
	}
	if atxTargetEpoch := atx.PubLayerID.GetEpoch() + 1; atxTargetEpoch != blockEpoch {
		return nil, fmt.Errorf("ATX target epoch (%d) doesn't match block publication epoch (%d)",
			atxTargetEpoch, blockEpoch)
	}
	if pubString := block.MinerID().String(); atx.NodeID.Key != pubString {
		return nil, fmt.Errorf("block vrfsgn (%s) mismatch with ATX node (%s)", pubString, atx.NodeID.Key)
	}
	return atx, nil
}
