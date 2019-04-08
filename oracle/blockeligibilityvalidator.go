package oracle

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/sha256-simd"
)

type VRFValidationFunction func(message, signature, publicKey []byte) error

type BlockEligibilityValidator struct {
	committeeSize  int32
	layersPerEpoch uint16
	activationDb   ActivationDb
	beaconProvider *EpochBeaconProvider
	validateVRF    VRFValidationFunction
}

func NewBlockEligibilityValidator(committeeSize int32, layersPerEpoch uint16, activationDb ActivationDb,
	beaconProvider *EpochBeaconProvider, validateVRF VRFValidationFunction) *BlockEligibilityValidator {

	return &BlockEligibilityValidator{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDb,
		beaconProvider: beaconProvider,
		validateVRF:    validateVRF,
	}
}

func (v BlockEligibilityValidator) BlockEligible(block *mesh.Block) (bool, error) {
	epochNumber := block.LayerIndex.GetEpoch(v.layersPerEpoch)

	atx, err := v.activationDb.GetAtx(block.ATXID)
	if err != nil {
		log.Error("getting ATX failed: %v", err)
		return false, err
	}

	if err := atx.Validate(); err != nil {
		log.Error("ATX is invalid: %v", err)
		return false, err
	}
	if atxEpochNumber := atx.LayerIndex.GetEpoch(v.layersPerEpoch); epochNumber != atxEpochNumber {
		log.Error("ATX epoch (%d) doesn't match layer ID epoch (%d)", atxEpochNumber, epochNumber)
		return false, fmt.Errorf("activation epoch (%d) mismatch with layer epoch (%d)", atxEpochNumber,
			epochNumber)
	}

	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(atx.ActiveSetSize, v.committeeSize, v.layersPerEpoch)
	if err != nil {
		log.Error("failed to get number of eligible blocks: %v", err)
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
		log.Error("eligibility VRF validation failed: %v", err)
		return false, err
	}
	vrfHash := sha256.Sum256(vrfSig)
	eligibleLayer := calcEligibleLayer(epochNumber, v.layersPerEpoch, vrfHash)

	return block.LayerIndex == eligibleLayer, nil
}
