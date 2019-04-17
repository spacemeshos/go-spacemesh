package oracle

import (
	"encoding/binary"
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/sha256-simd"
)

type ActivationDb interface {
	GetNodeAtxIds(node types.NodeId) ([]types.AtxId, error)
	GetAtx(id types.AtxId) (*types.ActivationTx, error)
}

type MinerBlockOracle struct {
	committeeSize  int32
	layersPerEpoch uint16
	activationDb   ActivationDb
	beaconProvider *EpochBeaconProvider
	vrfSigner      *crypto.VRFSigner
	nodeID         types.NodeId

	proofsEpoch       types.EpochId
	eligibilityProofs map[types.LayerID][]types.BlockEligibilityProof
	atxID             types.AtxId
}

func NewMinerBlockOracle(committeeSize int32, layersPerEpoch uint16, activationDb ActivationDb,
	beaconProvider *EpochBeaconProvider, vrfSigner *crypto.VRFSigner, nodeId types.NodeId) *MinerBlockOracle {

	return &MinerBlockOracle{
		committeeSize:  committeeSize,
		layersPerEpoch: layersPerEpoch,
		activationDb:   activationDb,
		beaconProvider: beaconProvider,
		vrfSigner:      vrfSigner,
		nodeID:         nodeId,
		proofsEpoch:    ^types.EpochId(0),
	}
}

func (bo *MinerBlockOracle) BlockEligible(layerID types.LayerID) (types.AtxId, []types.BlockEligibilityProof, error) {

	epochNumber := layerID.GetEpoch(bo.layersPerEpoch)

	if bo.proofsEpoch != epochNumber {
		err := bo.calcEligibilityProofs(epochNumber)
		if err != nil {
			return types.AtxId{}, nil, err
		}
	}

	return bo.atxID, bo.eligibilityProofs[layerID], nil
}

func (bo *MinerBlockOracle) calcEligibilityProofs(epochNumber types.EpochId) error {
	epochBeacon := bo.beaconProvider.GetBeacon(epochNumber)

	latestATXID, err := getLatestATXID(bo.activationDb, bo.nodeID)
	if err != nil {
		log.Error("failed to get latest ATX: %v", err)
		return err
	}
	bo.atxID = latestATXID

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

	bo.eligibilityProofs = map[types.LayerID][]types.BlockEligibilityProof{}
	for counter := uint32(0); counter < numberOfEligibleBlocks; counter++ {
		message := serializeVRFMessage(epochBeacon, epochNumber, counter)
		vrfSig := bo.vrfSigner.Sign(message)
		vrfHash := sha256.Sum256(vrfSig)
		eligibleLayer := calcEligibleLayer(epochNumber, bo.layersPerEpoch, vrfHash)
		bo.eligibilityProofs[eligibleLayer] = append(bo.eligibilityProofs[eligibleLayer], types.BlockEligibilityProof{
			J:   counter,
			Sig: vrfSig,
		})
	}
	bo.proofsEpoch = epochNumber
	return nil
}

func calcEligibleLayer(epochNumber types.EpochId, layersPerEpoch uint16, vrfHash [32]byte) types.LayerID {
	epochOffset := uint64(epochNumber) * uint64(layersPerEpoch)
	vrfInteger := binary.LittleEndian.Uint64(vrfHash[:8])
	eligibleLayerRelativeToEpochStart := vrfInteger % uint64(layersPerEpoch)
	return types.LayerID(eligibleLayerRelativeToEpochStart + epochOffset)
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

func getLatestATXID(activationDb ActivationDb, nodeID types.NodeId) (types.AtxId, error) {
	atxIDs, err := activationDb.GetNodeAtxIds(nodeID)
	if err != nil {
		log.Error("getting node ATX IDs failed: %v", err)
		return types.AtxId{}, err
	}
	numOfActivations := len(atxIDs)
	if numOfActivations < 1 {
		log.Error("no activations found for node id \"%v\"", nodeID.Key)
		return types.AtxId{}, errors.New("no activations found")
	}
	latestATXID := atxIDs[numOfActivations-1]
	return latestATXID, err
}

func serializeVRFMessage(epochBeacon []byte, epochNumber types.EpochId, counter uint32) []byte {
	message := make([]byte, len(epochBeacon)+binary.Size(epochNumber)+binary.Size(counter))
	copy(message, epochBeacon)
	binary.LittleEndian.PutUint64(message[len(epochBeacon):], uint64(epochNumber))
	binary.LittleEndian.PutUint32(message[len(epochBeacon)+binary.Size(epochNumber):], counter)
	return message
}
