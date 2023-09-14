package miner

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errMinerHasNoATXInPreviousEpoch = errors.New("miner has no ATX in previous epoch")
	errZeroEpochWeight              = errors.New("zero total weight for epoch")
	errEmptyActiveSet               = errors.New("empty active set for epoch")
)

type EpochEligibility struct {
	Epoch     types.EpochID
	Atx       types.ATXID
	ActiveSet types.ATXIDList
	Proofs    map[types.LayerID][]types.VotingEligibility
	Slots     uint32
}

// Oracle provides proposal eligibility proofs for the miner.
type Oracle struct {
	cfg       config
	clock     layerClock
	cdb       *datastore.CachedDB
	vrfSigner *signing.VRFSigner
	syncState system.SyncStateProvider
	log       log.Log

	mu    sync.Mutex
	cache *EpochEligibility
}

func newMinerOracle(cfg config, c layerClock, cdb *datastore.CachedDB, vrfSigner *signing.VRFSigner, ss system.SyncStateProvider, log log.Log) *Oracle {
	return &Oracle{
		cfg:       cfg,
		clock:     c,
		cdb:       cdb,
		vrfSigner: vrfSigner,
		syncState: ss,
		log:       log,
		cache:     &EpochEligibility{},
	}
}

// ProposalEligibility returns the miner's ATXID and the active set for the layer's epoch, along with the list of eligibility
// proofs for that layer.
func (o *Oracle) ProposalEligibility(lid types.LayerID, beacon types.Beacon, nonce types.VRFPostIndex) (*EpochEligibility, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	epoch := lid.GetEpoch()
	if lid <= types.GetEffectiveGenesis() {
		o.log.With().Panic("eligibility should not be queried during genesis", lid, epoch)
	}

	o.log.With().Debug("asked for proposal eligibility",
		log.Stringer("requested epoch", epoch),
		log.Stringer("cached epoch", o.cache.Epoch),
	)

	var layerProofs []types.VotingEligibility
	if o.cache.Epoch == epoch { // use the cached value
		layerProofs = o.cache.Proofs[lid]
		o.log.With().Debug("got cached eligibility",
			log.Stringer("requested epoch", epoch),
			log.Stringer("cached epoch", o.cache.Epoch),
			log.Int("num proposals", len(layerProofs)),
		)
		return o.cache, nil
	}

	ee, err := o.calcEligibilityProofs(lid, epoch, beacon, nonce)
	if err != nil {
		return nil, err
	}
	events.EmitEligibilities(ee.Epoch, beacon, ee.Atx, uint32(len(ee.ActiveSet)), ee.Proofs)
	o.cache = ee
	return ee, nil
}

func (o *Oracle) activesFromFirstBlock(targetEpoch types.EpochID, ownAtx types.ATXID, ownWeight uint64) (uint64, []types.ATXID, error) {
	if ownAtx == types.EmptyATXID || ownWeight == 0 {
		o.log.Fatal("invalid miner atx")
	}

	activeSet, err := ActiveSetFromEpochFirstBlock(o.cdb, targetEpoch)
	if err != nil {
		return 0, nil, err
	}
	_, totalWeight, own, err := infoFromActiveSet(o.cdb, o.vrfSigner.NodeID(), activeSet)
	if err != nil {
		return 0, nil, err
	}
	if own == types.EmptyATXID {
		// miner is not included in the active set derived from the epoch's first block
		activeSet = append(activeSet, ownAtx)
		totalWeight += ownWeight
	}
	return totalWeight, activeSet, nil
}

func infoFromActiveSet(cdb *datastore.CachedDB, nodeID types.NodeID, activeSet []types.ATXID) (uint64, uint64, types.ATXID, error) {
	var (
		total, ownWeight uint64
		ownAtx           types.ATXID
	)
	for _, id := range activeSet {
		hdr, err := cdb.GetAtxHeader(id)
		if err != nil {
			return 0, 0, types.EmptyATXID, fmt.Errorf("new ndoe get atx hdr: %w", err)
		}
		if hdr.NodeID == nodeID {
			ownAtx = id
			ownWeight = hdr.GetWeight()
		}
		total += hdr.GetWeight()
	}
	return ownWeight, total, ownAtx, nil
}

func (o *Oracle) activeSet(targetEpoch types.EpochID) (uint64, uint64, types.ATXID, []types.ATXID, error) {
	var (
		ownWeight, totalWeight uint64
		ownAtx                 types.ATXID
		atxids                 []types.ATXID
	)

	epochStart := o.clock.LayerToTime(targetEpoch.FirstLayer())
	numOmitted := 0
	if err := o.cdb.IterateEpochATXHeaders(targetEpoch, func(header *types.ActivationTxHeader) error {
		if header.NodeID == o.cfg.nodeID {
			ownWeight = header.GetWeight()
			ownAtx = header.ID
		}
		grade, err := GradeAtx(o.cdb, header.NodeID, header.Received, epochStart, o.cfg.networkDelay)
		if err != nil {
			return err
		}
		if grade != Good && header.NodeID != o.cfg.nodeID {
			o.log.With().Info("atx omitted from active set",
				header.ID,
				log.Int("grade", int(grade)),
				log.Stringer("smesher", header.NodeID),
				log.Bool("own", header.NodeID == o.cfg.nodeID),
				log.Time("received", header.Received),
				log.Time("epoch_start", epochStart),
			)
			numOmitted++
			return nil
		}
		totalWeight += header.GetWeight()
		atxids = append(atxids, header.ID)
		return nil
	}); err != nil {
		return 0, 0, types.EmptyATXID, nil, err
	}

	if ownAtx == types.EmptyATXID || ownWeight == 0 {
		return 0, 0, types.EmptyATXID, nil, errMinerHasNoATXInPreviousEpoch
	}

	if total := numOmitted + len(atxids); total == 0 {
		return 0, 0, types.EmptyATXID, nil, errEmptyActiveSet
	} else if numOmitted*100/total > 100-o.cfg.goodAtxPct {
		// if the node is not synced during `targetEpoch-1`, it doesn't have the correct receipt timestamp
		// for all the atx and malfeasance proof. this active set is not usable.
		// TODO: change after timing info of ATXs and malfeasance proofs is sync'ed from peers as well
		var err error
		totalWeight, atxids, err = o.activesFromFirstBlock(targetEpoch, ownAtx, ownWeight)
		if err != nil {
			return 0, 0, types.EmptyATXID, nil, err
		}
		o.log.With().Info("miner not synced during prior epoch, active set from first block",
			log.Int("all atx", total),
			log.Int("num omitted", numOmitted),
			log.Int("num block atx", len(atxids)),
		)
	} else {
		o.log.With().Info("active set selected for proposal",
			log.Int("num atx", len(atxids)),
			log.Int("num omitted", numOmitted),
			log.Int("min atx good pct", o.cfg.goodAtxPct),
		)
	}
	return ownWeight, totalWeight, ownAtx, atxids, nil
}

// calcEligibilityProofs calculates the eligibility proofs of proposals for the miner in the given epoch
// and returns the proofs along with the epoch's active set.
func (o *Oracle) calcEligibilityProofs(lid types.LayerID, epoch types.EpochID, beacon types.Beacon, nonce types.VRFPostIndex) (*EpochEligibility, error) {
	ref, err := ballots.RefBallot(o.cdb, epoch, o.vrfSigner.NodeID())
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, err
	}

	var (
		minerWeight, totalWeight uint64
		ownAtx                   types.ATXID
		activeSet                []types.ATXID
	)
	if ref == nil {
		minerWeight, totalWeight, ownAtx, activeSet, err = o.activeSet(epoch)
	} else {
		activeSet = ref.ActiveSet
		o.log.With().Info("use active set from ref ballot",
			ref.ID(),
			log.Int("num atx", len(activeSet)),
		)
		minerWeight, totalWeight, ownAtx, err = infoFromActiveSet(o.cdb, o.vrfSigner.NodeID(), activeSet)
	}
	if err != nil {
		return nil, err
	}
	if totalWeight == 0 {
		return nil, errZeroEpochWeight
	}
	if len(activeSet) == 0 {
		return nil, errEmptyActiveSet
	}
	o.log.With().Debug("calculating eligibility",
		epoch,
		beacon,
		log.Uint64("weight", minerWeight),
		log.Uint64("total weight", totalWeight),
	)
	var numEligibleSlots uint32
	if ref == nil {
		numEligibleSlots, err = proposals.GetNumEligibleSlots(minerWeight, o.cfg.minActiveSetWeight, totalWeight, o.cfg.layerSize, o.cfg.layersPerEpoch)
		if err != nil {
			return nil, fmt.Errorf("oracle get num slots: %w", err)
		}
	} else {
		numEligibleSlots = ref.EpochData.EligibilityCount
	}

	eligibilityProofs := map[types.LayerID][]types.VotingEligibility{}
	for counter := uint32(0); counter < numEligibleSlots; counter++ {
		message, err := proposals.SerializeVRFMessage(beacon, epoch, nonce, counter)
		if err != nil {
			o.log.With().Fatal("failed to serialize VRF msg", log.Err(err))
		}
		vrfSig := o.vrfSigner.Sign(message)
		eligibleLayer := proposals.CalcEligibleLayer(epoch, o.cfg.layersPerEpoch, vrfSig)
		eligibilityProofs[eligibleLayer] = append(eligibilityProofs[eligibleLayer], types.VotingEligibility{
			J:   counter,
			Sig: vrfSig,
		})
		o.log.Debug(fmt.Sprintf("signed vrf message, counter: %v, vrfSig: %s, layer: %v", counter, vrfSig, eligibleLayer))
	}

	o.log.With().Info("proposal eligibility for an epoch",
		epoch,
		beacon,
		log.Uint64("weight", minerWeight),
		log.Uint64("min activeset weight", o.cfg.minActiveSetWeight),
		log.Uint64("total weight", totalWeight),
		log.Uint32("total num slots", numEligibleSlots),
		log.Int("num layers eligible", len(eligibilityProofs)),
		log.Array("layers to num proposals", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
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
	sort.Slice(activeSet, func(i, j int) bool {
		return bytes.Compare(activeSet[i].Bytes(), activeSet[j].Bytes()) < 0
	})
	return &EpochEligibility{
		Epoch:     epoch,
		Atx:       ownAtx,
		ActiveSet: activeSet,
		Proofs:    eligibilityProofs,
		Slots:     numEligibleSlots,
	}, nil
}
