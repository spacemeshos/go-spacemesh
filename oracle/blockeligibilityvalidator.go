package oracle

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/sha256-simd"
)

type VRFValidationFunction func(message, signature, publicKey []byte) error

type BlockEligibilityValidator struct {
	committeeSize  int32
	layersPerEpoch uint16
	activationDb   ActivationDb
	beaconProvider *EpochBeaconProvider
	validateVRF    VRFValidationFunction
	log            log.Log
}

func NewBlockEligibilityValidator(committeeSize int32, layersPerEpoch uint16, activationDb ActivationDb,
	beaconProvider *EpochBeaconProvider, validateVRF VRFValidationFunction, log log.Log) *BlockEligibilityValidator {

	return &BlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDb,
		beaconProvider: beaconProvider,
		validateVRF:    validateVRF,
		log:            log,
	}
}

func (v BlockEligibilityValidator) BlockEligible(block *types.Block) (bool, error) {
	epochNumber := block.LayerIndex.GetEpoch(v.layersPerEpoch)

	activeSetSize, err := v.getActiveSetSize(epochNumber, block)
	if err != nil {
		return false, err
	}

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
	err = v.validateVRF(message, vrfSig, []byte(block.MinerID.VRFPublicKey))
	if err != nil {
		v.log.Error("eligibility VRF validation failed: %v", err)
		return false, err
	}
	vrfHash := sha256.Sum256(vrfSig)
	eligibleLayer := calcEligibleLayer(epochNumber, v.layersPerEpoch, vrfHash)

	return block.LayerIndex == eligibleLayer, nil
}

func (v BlockEligibilityValidator) getActiveSetSize(epochNumber types.EpochId, block *types.Block) (uint32, error) {
	if epochNumber.IsGenesis() {
		return GenesisActiveSetSize, nil
	}
	atx, err := v.activationDb.GetAtx(block.ATXID)
	if err != nil {
		v.log.Error("getting ATX failed: %v", err)
		return 0, err
	}
	if !atx.Valid {
		v.log.Error("ATX is invalid: %v", err)
		return 0, err
	}
	if atxEpochNumber := atx.LayerIdx.GetEpoch(v.layersPerEpoch); epochNumber != atxEpochNumber {
		v.log.Error("ATX epoch (%d) doesn't match layer ID epoch (%d)", atxEpochNumber, epochNumber)
		return 0, fmt.Errorf("activation epoch (%d) mismatch with layer epoch (%d)", atxEpochNumber,
			epochNumber)
	}
	activeSetSize := atx.ActiveSetSize
	return activeSetSize, err
}
