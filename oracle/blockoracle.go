package oracle

import (
	"encoding/binary"
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/sha256-simd"
)

type ActivationDb interface {
	GetNodeAtxIds(node mesh.NodeId) ([]mesh.AtxId, error)
	GetAtx(id mesh.AtxId) (*mesh.ActivationTx, error)
}

type MinerBlockOracle struct {
	committeeSize  int32
	layersPerEpoch uint16
	activationDb   ActivationDb
	beaconProvider *EpochBeaconProvider
	vrfSigner      *crypto.VRFSigner
	nodeID         mesh.NodeId

	eligibilityProofs map[mesh.LayerID][]mesh.BlockEligibilityProof
	proofsEpoch       uint64
}

func NewMinerBlockOracle(committeeSize int32, layersPerEpoch uint16, activationDb ActivationDb,
	beaconProvider *EpochBeaconProvider, vrfSigner *crypto.VRFSigner, nodeId mesh.NodeId) *MinerBlockOracle {

	return &MinerBlockOracle{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDb,
		beaconProvider: beaconProvider,
		vrfSigner:      vrfSigner,
		nodeID:         nodeId,
		proofsEpoch:    ^uint64(0),
	}
}

func (bo *MinerBlockOracle) BlockEligible(layerID mesh.LayerID) ([]mesh.BlockEligibilityProof, error) {

	epochNumber := layerID.GetEpoch(bo.layersPerEpoch)

	if bo.proofsEpoch != epochNumber {
		err := bo.calcEligibilityProofs(epochNumber)
		if err != nil {
			return nil, err
		}
	}

	return bo.eligibilityProofs[layerID], nil
}

func (bo *MinerBlockOracle) calcEligibilityProofs(epochNumber uint64) error {
	epochBeacon := bo.beaconProvider.GetBeacon(epochNumber)

	latestATXID, err := getLatestATXID(bo.activationDb, bo.nodeID)
	if err != nil {
		log.Error("failed to get latest ATX: %v", err)
		return err
	}

	atx, err := bo.activationDb.GetAtx(latestATXID)
	if err != nil {
		log.Error("getting ATX failed: %v", err)
		return err
	}

	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(atx.ActiveSetSize, bo.committeeSize, bo.layersPerEpoch)
	if err != nil {
		log.Error("failed to get number of eligible blocks: %v", err)
		return err
	}

	bo.eligibilityProofs = map[mesh.LayerID][]mesh.BlockEligibilityProof{}
	for counter := uint32(0); counter < numberOfEligibleBlocks; counter++ {
		message := serializeVRFMessage(epochBeacon, epochNumber, counter)
		sig := bo.vrfSigner.Sign(message)
		hash := sha256.Sum256(sig)
		epochOffset := epochNumber * uint64(bo.layersPerEpoch)
		eligibleLayer := mesh.LayerID(binary.LittleEndian.Uint64(hash[:])%uint64(bo.layersPerEpoch) + epochOffset)
		bo.eligibilityProofs[eligibleLayer] = append(bo.eligibilityProofs[eligibleLayer], mesh.BlockEligibilityProof{
			J:   counter,
			Sig: sig,
		})
	}
	return nil
}

func getNumberOfEligibleBlocks(activeSetSize uint32, committeeSize int32, layersPerEpoch uint16) (uint32, error) {
	if activeSetSize == 0 {
		log.Error("empty active set detected in activation transaction")
		return 0, errors.New("empty active set not allowed")
	}
	numberOfEligibleBlocks := uint32(committeeSize) * uint32(layersPerEpoch) / activeSetSize
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}
	return numberOfEligibleBlocks, nil
}

func getLatestATXID(activationDb ActivationDb, nodeID mesh.NodeId) (mesh.AtxId, error) {
	atxIDs, err := activationDb.GetNodeAtxIds(nodeID)
	if err != nil {
		log.Error("getting node ATX IDs failed: %v", err)
		return mesh.AtxId{}, err
	}
	numOfActivations := len(atxIDs)
	if numOfActivations < 1 {
		log.Error("no activations found for node id \"%v\"", nodeID.Key)
		return mesh.AtxId{}, errors.New("no activations found")
	}
	latestATXID := atxIDs[numOfActivations-1]
	return latestATXID, err
}

func serializeVRFMessage(epochBeacon []byte, epochNumber uint64, counter uint32) []byte {
	message := make([]byte, len(epochBeacon)+binary.Size(epochNumber)+binary.Size(counter))
	copy(message, epochBeacon)
	binary.LittleEndian.PutUint64(message[len(epochBeacon):], epochNumber)
	binary.LittleEndian.PutUint32(message[len(epochBeacon)+binary.Size(epochNumber):], counter)
	return message
}
