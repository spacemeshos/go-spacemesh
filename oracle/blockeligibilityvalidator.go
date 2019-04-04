package oracle

import (
	"encoding/binary"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
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

func (v MinerBlockEligibilityValidator) BlockEligible(layerID mesh.LayerID, nodeID mesh.NodeId,
	proof mesh.BlockEligibilityProof, atxID mesh.AtxId) (bool, error) {

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
	message := make([]byte, len(epochBeacon)+binary.Size(epochNumber)+binary.Size(counter))
	copy(message, epochBeacon)
	binary.LittleEndian.PutUint64(message[len(epochBeacon):], epochNumber)
	binary.LittleEndian.PutUint32(message[len(epochBeacon)+binary.Size(epochNumber):], counter)
	sig := proof.Sig
	err = v.validateVRF(message, sig, []byte(nodeID.Vrf))
	if err != nil {
		log.Error("eligibility VRF validation failed: %v", err)
		return false, err
	}
	hash := sha256.Sum256(sig)
	epochOffset := epochNumber * uint64(v.layersPerEpoch)
	eligibleLayer := mesh.LayerID(binary.LittleEndian.Uint64(hash[:])%uint64(v.layersPerEpoch) + epochOffset)

	return layerID == eligibleLayer, nil
}
