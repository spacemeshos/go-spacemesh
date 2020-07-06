package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type Committee struct {
	actives map[string]uint64 // weights of all active nodes

	Size   int     // committee size aka N
	Weight uint64  // committee messages count/weight expectation aka F+1
}

func calcCommitteeWeight(actives map[string]uint64, committeeSize int) uint64 {
	total := uint64(0)
	if len(actives) > 0 {
		for _, w := range actives {
			// TODO: it must calculate total wights for all eligible or oll active nodes?
			total += w // total weight of all active nodes
		}

		// weight threshold as avg_weight * committee_size
		// TODO: avg weight must be MEAN or MEDIAN?
		// TODO: since weights of eligible nodes can be very different
		//       it may be a good idea to normalize weights as tgh(w/median)*avg
		return total * uint64(committeeSize) / uint64(len(actives))
	}
	return 0
}

func (ws Committee) Fill(oracle Rolacle, logger log.Log, layerId types.LayerID, committeeSize int) error {
	weighted, ok := oracle.(interface {
		Actives(layer types.LayerID) (map[string]uint64, error)
	})
	if !ok {
		logger.Error("oracle does not have Actives function")
		return startInstanceError(errors.New("oracle does not have Actives function"))
	}
	actives, err := weighted.Actives(layerId)
	if err != nil {
		logger.Error("failed to get active nodes for the layer: %v", err.Error())
		return startInstanceError(errors.New("instance started with no active nodes"))
	}
	if len(actives) > 0 {
		ws.actives = actives
		ws.Weight = calcCommitteeWeight(actives, committeeSize)
	}
	ws.Size = committeeSize
	return nil
}

func (ws Committee) WeightOf(pub string) uint64 {
	if len(ws.actives) > 0 {
		return ws.actives[pub]
	}
	return 1
}

func (ws Committee) Threshold() uint64 {
	return ws.Weight
}

// WeightTracker tracks weight of references of any object id.
type WeightTracker map[interface{}]uint64

// WeightOf returns the weight of the given id.
func (tracker WeightTracker) WeightOf(id interface{}) uint64 {
	return tracker[id]
}

// Track increases the count for the given object id.
func (tracker WeightTracker) Track(id interface{}, weight uint64) {
	tracker[id] += weight
}

// CountStatus for tests compatibility
func (tracker WeightTracker) CountStatus(id interface{}) uint32 {
	return uint32(tracker[id])
}
