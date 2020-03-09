package oracle

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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
	var activeSetSize uint32
	var vrfPubkey []byte
	var genesisNoAtx bool

	epochNumber := block.LayerIndex.GetEpoch(v.layersPerEpoch)
	if epochNumber == 0 {
		v.log.With().Warning("skipping epoch 0 block validation.",
			log.BlockId(block.Id().String()), log.LayerId(block.LayerIndex.Uint64()))
		return true, nil
	}
	if block.ATXID == *types.EmptyAtxId {
		if !epochNumber.IsGenesis() {
			return false, fmt.Errorf("no associated ATX in epoch %v", epochNumber)
		}
		genesisNoAtx = true
	} else {
		atx, err := v.getValidAtx(block)
		if err != nil {
			return false, err
		}
		activeSetSize, vrfPubkey = atx.ActiveSetSize, atx.NodeId.VRFPublicKey
	}
	if epochNumber.IsGenesis() {
		v.log.With().Info("using genesisActiveSetSize",
			log.BlockId(block.ShortString()), log.Uint32("genesisActiveSetSize", v.genesisActiveSetSize))
		activeSetSize = v.genesisActiveSetSize
	}

	numberOfEligibleBlocks, err := getNumberOfEligibleBlocks(activeSetSize, v.committeeSize, v.layersPerEpoch)
	if err != nil {
		return false, fmt.Errorf("failed to get number of eligible blocks: %v", err)
	}

	counter := block.EligibilityProof.J
	if counter >= numberOfEligibleBlocks {
		return false, fmt.Errorf("proof counter (%d) must be less than number of eligible blocks (%d)", counter,
			numberOfEligibleBlocks)
	}

	epochBeacon := v.beaconProvider.GetBeacon(epochNumber)
	message := serializeVRFMessage(epochBeacon, epochNumber, counter)
	vrfSig := block.EligibilityProof.Sig

	if genesisNoAtx {
		v.log.Info("skipping VRF validation, genesis block with no ATX") // TODO
		return true, nil
	}
	res, err := v.validateVRF(message, vrfSig, vrfPubkey)
	if err != nil {
		return false, fmt.Errorf("eligibility VRF validation failed: %v", err)
	}

	if !res {
		return false, fmt.Errorf("eligibility VRF validation failed")
	}

	eligibleLayer := calcEligibleLayer(epochNumber, v.layersPerEpoch, sha256.Sum256(vrfSig))

	if block.LayerIndex != eligibleLayer {
		return false, fmt.Errorf("block layer (%v) does not match eligibility layer (%v)",
			block.LayerIndex, eligibleLayer)
	}
	return true, nil
}

func (v BlockEligibilityValidator) getValidAtx(block *types.Block) (*types.ActivationTxHeader, error) {
	blockEpoch := block.LayerIndex.GetEpoch(v.layersPerEpoch)
	atx, err := v.activationDb.GetAtxHeader(block.ATXID)
	if err != nil {
		return nil, fmt.Errorf("getting ATX failed: %v %v ep(%v)", err, block.ATXID.ShortString(), blockEpoch)
	}
	if atxTargetEpoch := atx.PubLayerIdx.GetEpoch(v.layersPerEpoch) + 1; atxTargetEpoch != blockEpoch {
		return nil, fmt.Errorf("ATX target epoch (%d) doesn't match block publication epoch (%d)",
			atxTargetEpoch, blockEpoch)
	}
	if pubString := block.MinerId().String(); atx.NodeId.Key != pubString {
		return nil, fmt.Errorf("block signer (%s) mismatch with ATX node (%s)", pubString, atx.NodeId.Key)
	}
	return atx, nil
}
