package oracle

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/sha256-simd"
)

type VRFValidationFunction func(message, signature, publicKey []byte) (bool, error)

type BlockEligibilityValidator struct {
	committeeSize  uint32
	layersPerEpoch uint16
	activationDb   ActivationDb
	beaconProvider *EpochBeaconProvider
	validateVRF    VRFValidationFunction
	log            log.Log
}

func NewBlockEligibilityValidator(committeeSize int32, layersPerEpoch uint16, activationDb ActivationDb,
	beaconProvider *EpochBeaconProvider, validateVRF VRFValidationFunction, log log.Log) *BlockEligibilityValidator {

	return &BlockEligibilityValidator{
		committeeSize:  uint32(committeeSize),
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDb,
		beaconProvider: beaconProvider,
		validateVRF:    validateVRF,
		log:            log,
	}
}

func (v BlockEligibilityValidator) BlockEligible(block *types.BlockHeader) (bool, error) {
	epochNumber := block.LayerIndex.GetEpoch(v.layersPerEpoch)

	// need to get active set size from previous epoch
	activeSetSize, err := v.getActiveSetSize(block)
	if err != nil {
		return false, err
	}

	v.log.Info("Getting number of eligible blocks using params activeSetSize: %v, committeeSize: %v, layersPerEpoch: %v", activeSetSize, v.committeeSize, v.layersPerEpoch)
	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(activeSetSize, v.committeeSize, v.layersPerEpoch, v.log)
	if err != nil {
		v.log.Error("failed to get number of eligible blocks: %v", err)
		return false, err
	}

	counter := block.EligibilityProof.J
	if counter >= numberOfEligibleBlocks {
		return false, fmt.Errorf("proof counter (%d) must be less than number of eligible blocks (%d)", counter,
			numberOfEligibleBlocks)
	}

	epochBeacon := v.beaconProvider.GetBeacon(epochNumber)
	message := serializeVRFMessage(epochBeacon, epochNumber, counter)
	vrfSig := block.EligibilityProof.Sig
	res, err := v.validateVRF(message, vrfSig, []byte(block.MinerID.VRFPublicKey))
	if err != nil {
		v.log.Error("eligibility VRF validation erred: %v", err)
		return false, fmt.Errorf("eligibility VRF validation failed: %v", err)
	}
	if !res {
		v.log.Error("eligibility VRF validation failed")
		return false, nil
	}
	vrfHash := sha256.Sum256(vrfSig)
	eligibleLayer := calcEligibleLayer(epochNumber, v.layersPerEpoch, vrfHash)

	return block.LayerIndex == eligibleLayer, nil
}

func (v BlockEligibilityValidator) getActiveSetSize(block *types.BlockHeader) (uint32, error) {
	blockEpoch := block.LayerIndex.GetEpoch(v.layersPerEpoch)
	if blockEpoch.IsGenesis() {
		return v.committeeSize, nil
	}
	atx, err := v.activationDb.GetAtx(block.ATXID)
	if err != nil {
		v.log.Error("getting ATX failed: %v %v ep(%v)", err, block.ATXID.ShortString(), blockEpoch)
		return 0, fmt.Errorf("getting ATX failed: %v %v ep(%v)", err, block.ATXID.ShortString(), blockEpoch)
	}
	// TODO: remove the following check, as it should be validated before calling BlockEligible
	if atxTargetEpoch := atx.PubLayerIdx.GetEpoch(v.layersPerEpoch) + 1; atxTargetEpoch != blockEpoch {
		v.log.Error("ATX target epoch (%d) doesn't match block publication epoch (%d)",
			atxTargetEpoch, blockEpoch)
		return 0, fmt.Errorf("ATX target epoch (%d) doesn't match block publication epoch (%d)",
			atxTargetEpoch, blockEpoch)
	}
	return atx.ActiveSetSize, nil
}
