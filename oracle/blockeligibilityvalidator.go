package oracle

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/block"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/sha256-simd"
)

type VRFValidationFunction func(message, signature, publicKey []byte) error

type MinerBlockEligibilityValidator struct {
	committeeSize  int32
	layersPerEpoch uint16
	activationDb   ActivationDb
	beaconProvider *EpochBeaconProvider
	validateVRF    VRFValidationFunction
}

func (v MinerBlockEligibilityValidator) BlockEligible(layerID block.LayerID, nodeID block.NodeId,
	proof block.BlockEligibilityProof, atxID block.AtxId) (bool, error) {

	epochNumber := layerID.GetEpoch(v.layersPerEpoch)

	atx, err := v.activationDb.GetAtx(atxID)
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

	counter := proof.J
	if counter >= numberOfEligibleBlocks {
		return false, fmt.Errorf("proof counter (%d) must be less than number of eligible blocks (%d)", counter,
			numberOfEligibleBlocks)
	}

	epochBeacon := v.beaconProvider.GetBeacon(epochNumber)
	message := serializeVRFMessage(epochBeacon, epochNumber, counter)
	vrfSig := proof.Sig
	err = v.validateVRF(message, vrfSig, []byte(nodeID.VRFPublicKey))
	if err != nil {
		log.Error("eligibility VRF validation failed: %v", err)
		return false, err
	}
	vrfHash := sha256.Sum256(vrfSig)
	eligibleLayer := calcEligibleLayer(epochNumber, v.layersPerEpoch, vrfHash)

	return layerID == eligibleLayer, nil
}
