package oracle

import (
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"go.uber.org/zap"
)

type Opt func(*Oracle)

func WithConfidence(interval types.LayerID) Opt {
	return func(o *Oracle) {
		o.confidence = interval
	}
}

func WithLogger(log *zap.Logger) Opt {
	return func(o *Oracle) {
		o.log = log
	}
}

func New(db *sql.Database, atxsdata *atxsdata.Data, opts ...Opt) *Oracle {
	o := &Oracle{
		log:        zap.NewNop(),
		db:         db,
		atxsdata:   atxsdata,
		sets:       map[types.EpochID]set{},
		confidence: 200,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

type set struct {
	set    map[types.NodeID]types.ATXID
	weight uint64
}

type Oracle struct {
	log        *zap.Logger
	db         *sql.Database
	atxsdata   *atxsdata.Data
	confidence types.LayerID

	mu   sync.Mutex
	last types.EpochID
	sets map[types.EpochID]set
}

func (o *Oracle) IsActive(
	signer *signing.EdSigner,
	beacon types.Beacon,
	lid types.LayerID,
	round uint32,
) (*types.HareEligibility, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	target := o.target(lid)
	o.last = max(target, o.last)
	if o.last-target > 1 {
		return nil, fmt.Errorf("oracle is ahead of target epoch %v > %v", o.last, target)
	}
	if _, exists := o.sets[target]; !exists {
		if err := o.recomputeFor(target); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (o *Oracle) recomputeFor(epoch types.EpochID) error {
	o.log.Debug("recomputing oracle data for epoch", zap.Uint64("epoch", uint64(epoch)))
	return nil
}

func (o *Oracle) target(lid types.LayerID) types.EpochID {
	target := lid.GetEpoch()
	// the first bootstrap data targets first epoch after genesis (epoch 2)
	// and the epoch where checkpoint recovery happens
	if target > types.GetEffectiveGenesis().Add(1).GetEpoch() &&
		lid.Difference(target.FirstLayer()) < uint32(o.confidence) {
		target -= 1
	}
	return target
}
