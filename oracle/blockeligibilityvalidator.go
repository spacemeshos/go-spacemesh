package oracle

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/sha256-simd"
)

type VRFValidationFunction func(message, signature, publicKey []byte) (bool, error)

type BlockEligibilityValidator struct {
	committeeSize        uint32
	genesisActiveSetSize uint32
	layersPerEpoch       uint16
	activationDb         ActivationDb
	beaconProvider       *EpochBeaconProvider
	validateVRF          VRFValidationFunction
	log                  log.Log
}

func NewBlockEligibilityValidator(committeeSize, genesisActiveSetSize uint32, layersPerEpoch uint16, activationDb ActivationDb,
	beaconProvider *EpochBeaconProvider, validateVRF VRFValidationFunction, log log.Log) *BlockEligibilityValidator {

	return &BlockEligibilityValidator{
		committeeSize:        committeeSize,
		genesisActiveSetSize: genesisActiveSetSize,
		layersPerEpoch:       layersPerEpoch,
		activationDb:         activationDb,
		beaconProvider:       beaconProvider,
		validateVRF:          validateVRF,
		log:                  log,
	}
}

func (v BlockEligibilityValidator) BlockSignedAndEligible(block *types.Block) (bool, error) {

	//todo remove this hack when genesis is handled
	if block.Layer().GetEpoch(v.layersPerEpoch) == 0 {
		return true, nil
	}

	//check block signature and is identity active
	pubKey, err := ed25519.ExtractPublicKey(block.Bytes(), block.Sig())
	if err != nil {
		return false, err
	}

	pubString := signing.NewPublicKey(pubKey).String()

	nodeId, active, atxid, err := v.activationDb.IsIdentityActive(pubString, block.Layer())
	if err != nil {
		return false, errors.New(fmt.Sprintf("error while checking IsIdentityActive for %v %v", block.ID(), err))
	}

	if !active {
		return false, errors.New(fmt.Sprintf("block %v identity activation check failed (ignore if the publication layer is in genesis)", block.ID()))
	}

	if atxid != block.ATXID {
		return false, errors.New(fmt.Sprintf("wrong associated atx got %v expected %v ", block.ATXID.ShortString(), atxid.ShortString()))
	}

	epochNumber := block.LayerIndex.GetEpoch(v.layersPerEpoch)

	// need to get active set size from previous epoch
	activeSetSize, err := v.getActiveSetSize(&block.BlockHeader)
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

	res, err := v.validateVRF(message, vrfSig, []byte(nodeId.VRFPublicKey))
	if err != nil {
		v.log.Error("eligibility VRF validation erred: %v", err)
		return false, fmt.Errorf("eligibility VRF validation failed: %v", err)
	}

	if !res {
		v.log.Error("eligibility VRF validation failed")
		return false, nil
	}

	eligibleLayer := calcEligibleLayer(epochNumber, v.layersPerEpoch, sha256.Sum256(vrfSig))

	return block.LayerIndex == eligibleLayer, nil
}

func (v BlockEligibilityValidator) getActiveSetSize(block *types.BlockHeader) (uint32, error) {
	blockEpoch := block.LayerIndex.GetEpoch(v.layersPerEpoch)
	if blockEpoch.IsGenesis() {
		return v.genesisActiveSetSize, nil
	}
	atx, err := v.activationDb.GetAtx(block.ATXID)
	if err != nil {
		v.log.Error("getting ATX failed: %v %v ep(%v)", err, block.ATXID.ShortString(), blockEpoch)
		return 0, fmt.Errorf("getting ATX failed: %v %v ep(%v)", err, block.ATXID.ShortString(), blockEpoch)
	}
	// TODO: remove the following check, as it should be validated before calling BlockSignedAndEligible
	if atxTargetEpoch := atx.PubLayerIdx.GetEpoch(v.layersPerEpoch) + 1; atxTargetEpoch != blockEpoch {
		v.log.Error("ATX target epoch (%d) doesn't match block publication epoch (%d)",
			atxTargetEpoch, blockEpoch)
		return 0, fmt.Errorf("ATX target epoch (%d) doesn't match block publication epoch (%d)",
			atxTargetEpoch, blockEpoch)
	}
	return atx.ActiveSetSize, nil
}
