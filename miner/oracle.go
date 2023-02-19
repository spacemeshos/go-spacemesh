package miner

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

var (
	errMinerHasNoATXInPreviousEpoch = errors.New("miner has no ATX in previous epoch")
	errZeroEpochWeight              = errors.New("zero total weight for epoch")
	errEmptyActiveSet               = errors.New("empty active set for epoch")
)

type oracleCache struct {
	epoch     types.EpochID
	atx       *types.ActivationTxHeader
	activeSet []types.ATXID
	proofs    map[types.LayerID][]types.VotingEligibility
}

// Oracle provides proposal eligibility proofs for the miner.
type Oracle struct {
	avgLayerSize   uint32
	layersPerEpoch uint32
	cdb            *datastore.CachedDB

	vrfSigner *signing.VRFSigner
	nodeID    types.NodeID
	log       log.Log

	mu    sync.Mutex
	cache oracleCache
}

func newMinerOracle(layerSize, layersPerEpoch uint32, cdb *datastore.CachedDB, vrfSigner *signing.VRFSigner, nodeID types.NodeID, log log.Log) *Oracle {
	return &Oracle{
		avgLayerSize:   layerSize,
		layersPerEpoch: layersPerEpoch,
		cdb:            cdb,
		vrfSigner:      vrfSigner,
		nodeID:         nodeID,
		log:            log,
	}
}

// GetProposalEligibility returns the miner's ATXID and the active set for the layer's epoch, along with the list of eligibility
// proofs for that layer.
func (o *Oracle) GetProposalEligibility(lid types.LayerID, beacon types.Beacon, nonce types.VRFPostIndex) (types.ATXID, []types.ATXID, []types.VotingEligibility, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	epoch := lid.GetEpoch()
	logger := o.log.WithFields(lid,
		log.Named("requested_epoch", epoch),
		log.Named("cached_epoch", o.cache.epoch))

	if epoch.IsGenesis() {
		logger.With().Panic("eligibility should not be queried during genesis", lid, epoch)
	}

	logger.Info("asked for proposal eligibility")

	var layerProofs []types.VotingEligibility
	if o.cache.epoch == epoch { // use the cached value
		layerProofs = o.cache.proofs[lid]
		logger.With().Info("got cached eligibility", log.Int("num_proposals", len(layerProofs)))
		return o.cache.atx.ID, o.cache.activeSet, layerProofs, nil
	}

	// calculate the proof
	atx, err := o.getOwnEpochATX(epoch)
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			return *types.EmptyATXID, nil, nil, errMinerHasNoATXInPreviousEpoch
		}
		return *types.EmptyATXID, nil, nil, fmt.Errorf("failed to get valid atx for node for target epoch %d: %w", epoch, err)
	}

	newProofs, activeSet, err := o.calcEligibilityProofs(atx.GetWeight(), epoch, beacon, nonce)
	if err != nil {
		logger.With().Error("failed to calculate eligibility proofs", log.Err(err))
		return *types.EmptyATXID, nil, nil, err
	}

	o.cache = oracleCache{
		epoch:     epoch,
		atx:       atx,
		activeSet: activeSet,
		proofs:    newProofs,
	}

	layerProofs = o.cache.proofs[lid]
	logger.With().Info("got eligibility for proposals", log.Int("num_proposals", len(layerProofs)))

	return o.cache.atx.ID, o.cache.activeSet, layerProofs, nil
}

func (o *Oracle) getOwnEpochATX(targetEpoch types.EpochID) (*types.ActivationTxHeader, error) {
	publishEpoch := targetEpoch - 1
	atxID, err := atxs.GetIDByEpochAndNodeID(o.cdb, publishEpoch, o.nodeID)
	if err != nil {
		o.log.With().Warning("failed to find ATX ID for node",
			log.Named("publish_epoch", publishEpoch),
			log.Named("smesher", o.nodeID),
			log.Err(err))
		return nil, fmt.Errorf("get ATX ID: %w", err)
	}

	atx, err := o.cdb.GetAtxHeader(atxID)
	if err != nil {
		o.log.With().Error("failed to get ATX header",
			log.Named("publish_epoch", publishEpoch),
			log.Named("smesher", o.nodeID),
			log.Err(err))
		return nil, fmt.Errorf("get ATX header: %w", err)
	}
	return atx, nil
}

// calcEligibilityProofs calculates the eligibility proofs of proposals for the miner in the given epoch
// and returns the proofs along with the epoch's active set.
func (o *Oracle) calcEligibilityProofs(weight uint64, epoch types.EpochID, beacon types.Beacon, nonce types.VRFPostIndex) (map[types.LayerID][]types.VotingEligibility, []types.ATXID, error) {
	logger := o.log.WithFields(epoch, beacon, log.Uint64("weight", weight))

	// get the previous epoch's total weight
	totalWeight, activeSet, err := o.cdb.GetEpochWeight(epoch)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get epoch %v weight: %w", epoch, err)
	}
	if totalWeight == 0 {
		return nil, nil, errZeroEpochWeight
	}
	if len(activeSet) == 0 {
		return nil, nil, errEmptyActiveSet
	}

	logger = logger.WithFields(log.Uint64("total_weight", totalWeight))
	logger.Info("calculating eligibility")

	numEligibleSlots, err := proposals.GetNumEligibleSlots(weight, totalWeight, o.avgLayerSize, o.layersPerEpoch)
	if err != nil {
		logger.With().Error("failed to get number of eligible proposals", log.Err(err))
		return nil, nil, fmt.Errorf("oracle get num slots: %w", err)
	}

	eligibilityProofs := map[types.LayerID][]types.VotingEligibility{}
	for counter := uint32(0); counter < numEligibleSlots; counter++ {
		message, err := proposals.SerializeVRFMessage(beacon, epoch, nonce, counter)
		if err != nil {
			logger.With().Fatal("failed to serialize VRF msg", log.Err(err))
		}
		vrfSig := o.vrfSigner.Sign(message)
		eligibleLayer := proposals.CalcEligibleLayer(epoch, o.layersPerEpoch, vrfSig)
		eligibilityProofs[eligibleLayer] = append(eligibilityProofs[eligibleLayer], types.VotingEligibility{
			J:   counter,
			Sig: vrfSig,
		})
		logger.Debug("signed vrf message, counter: %v, vrfSig: %v, layer: %v",
			counter, types.BytesToHash(vrfSig).ShortString(), eligibleLayer)
	}

	logger.With().Info("proposal eligibility calculated",
		log.Uint32("total_num_slots", numEligibleSlots),
		log.Int("num_layers_eligible", len(eligibilityProofs)),
		log.Array("layers_to_num_proposals", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			// Sort the layer map to log the layer data in order
			keys := make([]types.LayerID, 0, len(eligibilityProofs))
			for k := range eligibilityProofs {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool {
				return keys[i].Before(keys[j])
			})
			for _, lyr := range keys {
				encoder.AppendObject(log.ObjectMarshallerFunc(func(encoder log.ObjectEncoder) error {
					encoder.AddUint32("layer", lyr.Uint32())
					encoder.AddInt("slots", len(eligibilityProofs[lyr]))
					return nil
				}))
			}
			return nil
		})))
	return eligibilityProofs, activeSet, nil
}
