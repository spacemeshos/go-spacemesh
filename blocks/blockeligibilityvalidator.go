package blocks

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
)

// VRFValidationFunction is the VRF validation function.
type VRFValidationFunction func(publicKey, message, signature []byte) bool

// BlockEligibilityValidator holds all the dependencies for validating block eligibility.
type BlockEligibilityValidator struct {
	committeeSize  uint32
	layersPerEpoch uint32
	activationDb   activationDB
	blocks         blockDB
	beacons        beaconCollector
	validateVRF    VRFValidationFunction
	log            log.Log
}

// NewBlockEligibilityValidator returns a new BlockEligibilityValidator.
func NewBlockEligibilityValidator(
	committeeSize uint32, layersPerEpoch uint32, activationDb activationDB, beacons beaconCollector,
	validateVRF VRFValidationFunction, blockDB blockDB, log log.Log) *BlockEligibilityValidator {
	return &BlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDb,
		beacons:        beacons,
		validateVRF:    validateVRF,
		blocks:         blockDB,
		log:            log,
	}
}

// BlockSignedAndEligible checks that a given block is signed and eligible. It returns true with no error or false and
// an error that explains why validation failed.
func (v BlockEligibilityValidator) BlockSignedAndEligible(block *types.Block) (bool, error) {
	var weight, totalWeight uint64

	epochNumber := block.LayerIndex.GetEpoch()
	var err error
	activeSetBlock := block
	if block.RefBlock != nil {
		activeSetBlock, err = v.blocks.GetBlock(*block.RefBlock)
		if err != nil {
			// block should be present because we've synced it in the calling function
			return false, fmt.Errorf("cannot get reference block %v", *block.RefBlock)
		}
	}
	if activeSetBlock.ActiveSet == nil {
		return false, fmt.Errorf("failed to get active set from block %v", activeSetBlock.ID())
	}
	epochBeacon := activeSetBlock.TortoiseBeacon
	if epochBeacon == types.EmptyBeacon {
		return false, fmt.Errorf("failed to get tortoise beacon from block %v", activeSetBlock.ID())
	}
	// todo: optimize by using reference to active set size and cache active set size to not load all atxsIDs from db
	for _, atxID := range *activeSetBlock.ActiveSet {
		atxHeader, err := v.activationDb.GetAtxHeader(atxID)
		if err != nil {
			return false, fmt.Errorf("get ATX header: %w", err)
		}
		totalWeight += atxHeader.GetWeight()
	}
	if block.ATXID == *types.EmptyATXID {
		return false, fmt.Errorf("no associated ATX in epoch %v", epochNumber)
	}

	atx, err := v.getValidAtx(block)
	if err != nil {
		return false, err
	}
	weight = atx.GetWeight()
	vrfPubkey := atx.NodeID.VRFPublicKey

	numberOfEligibleBlocks, err := proposals.GetNumEligibleSlots(weight, totalWeight, v.committeeSize, v.layersPerEpoch)
	if err != nil {
		return false, fmt.Errorf("failed to get number of eligible blocks: %v", err)
	}

	counter := block.EligibilityProof.J
	if counter >= numberOfEligibleBlocks {
		return false, fmt.Errorf("proof counter (%d) must be less than number of eligible blocks (%d), totalWeight (%v)", counter,
			numberOfEligibleBlocks, totalWeight)
	}

	message, err := proposals.SerializeVRFMessage(epochBeacon, epochNumber, counter)
	if err != nil {
		return false, fmt.Errorf("validator serialize vrf: %w", err)
	}
	vrfSig := block.EligibilityProof.Sig

	beaconShortString := epochBeacon.ShortString()
	if !v.validateVRF(vrfPubkey, message, vrfSig) {
		return false, fmt.Errorf("tortoise beacon eligibility VRF validation failed: beacon %v, epoch %v, counter: %v, vrfSig: %v",
			beaconShortString, epochNumber, counter, types.BytesToHash(vrfSig).ShortString())
	}

	v.log.Info("validated tortoise beacon eligibility vrf of beacon %v in epoch %v (counter: %v)", beaconShortString, epochNumber, counter)

	eligibleLayer := proposals.CalcEligibleLayer(epochNumber, v.layersPerEpoch, vrfSig)

	if block.LayerIndex != eligibleLayer {
		return false, fmt.Errorf("block layer (%v) does not match eligibility layer (%v)",
			block.LayerIndex, eligibleLayer)
	}

	v.beacons.ReportBeaconFromBlock(epochNumber, block.ID(), epochBeacon, weight)
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
