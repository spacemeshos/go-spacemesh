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
	proof mesh.BlockEligibilityProof) (bool, error) {

	epochNumber := uint64(layerID) / uint64(v.layersPerEpoch)
	epochBeacon := v.beaconProvider.GetBeacon(epochNumber)

	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(nodeID, v.activationDb, v.committeeSize, v.layersPerEpoch)
	if err != nil {
		log.Error("failed to get number of eligible blocks: %v", err)
		return false, err
	}

	counter := proof.J
	if counter >= numberOfEligibleBlocks {
		return false, fmt.Errorf("proof counter (%d) must be less than number of eligible blocks (%d)", counter,
			numberOfEligibleBlocks)
	}

	message := make([]byte, len(epochBeacon)+binary.Size(epochNumber)+binary.Size(counter))
	copy(message, epochBeacon)
	binary.LittleEndian.PutUint64(message[len(epochBeacon):], epochNumber)
	binary.LittleEndian.PutUint32(message[len(epochBeacon)+binary.Size(epochNumber):], counter)
	sig := proof.Sig
	hash := sha256.Sum256(sig)
	eligibleLayer := mesh.LayerID(binary.LittleEndian.Uint64(hash[:]) % uint64(v.layersPerEpoch))

	return layerID == eligibleLayer, nil
}
