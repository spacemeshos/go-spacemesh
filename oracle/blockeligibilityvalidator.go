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
	activeSetSize := uint32(v.committeeSize)
	if !epochNumber.IsGenesis() {
		atx, err := v.getValidATX(epochNumber, block)
		if err != nil {
			return false, err
		}
		activeSetSize = atx.ActiveSetSize
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
	res, err := v.validateVRF(message, vrfSig, []byte(block.MinerID.VRFPublicKey))
	if err != nil {
		v.log.Error("eligibility VRF validation erred: %v", err)
		return false, fmt.Errorf("eligibility VRF validation failed: %v", err)
	}
	if !res {
		v.log.Error("eligibility VRF validation failed")
		// TODO: Noam - can we simply not return an error in this case?
		return false, fmt.Errorf("eligibility VRF validation failed")
	}
	vrfHash := sha256.Sum256(vrfSig)
	eligibleLayer := calcEligibleLayer(epochNumber, v.layersPerEpoch, vrfHash)

	return block.LayerIndex == eligibleLayer, nil
}

func (v BlockEligibilityValidator) getValidATX(blockEpoch types.EpochId, block *types.BlockHeader) (*types.ActivationTx, error) {
	atx, err := v.activationDb.GetAtx(block.ATXID)
	if err != nil {
		v.log.Error("getting ATX failed: %v %v ep(%v)", err, block.ATXID.String()[:5], blockEpoch)
		return nil, fmt.Errorf("getting ATX failed: %v %v ep(%v)", err, block.ATXID.String()[:5], blockEpoch)
	}
	if !atx.Valid {
		v.log.Error("ATX %v is invalid", atx.Id().String()[:5])
		return nil, fmt.Errorf("ATX %v is invalid", atx.Id().String()[:5])
	}
	if atxTargetEpoch := atx.PubLayerIdx.GetEpoch(v.layersPerEpoch) + 1; atxTargetEpoch != blockEpoch {
		v.log.Error("ATX target epoch (%d) doesn't match block publication epoch (%d)",
			atxTargetEpoch, blockEpoch)
		return nil, fmt.Errorf("ATX target epoch (%d) doesn't match block publication epoch (%d)",
			atxTargetEpoch, blockEpoch)
	}
	return atx, nil
}
