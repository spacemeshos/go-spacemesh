package beacon

import (
	"context"

	"golang.org/x/sync/singleflight"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type beaconService interface {
	Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error)
}

type BeaconCache struct {
	cache map[types.EpochID]types.Beacon
	sg    singleflight.Group
	svc   beaconService
}

func NewBeaconCache(svc beaconService) *BeaconCache {
	return &BeaconCache{
		svc:   svc,
		cache: make(map[types.EpochID]types.Beacon),
	}
}

func (b *BeaconCache) Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error) {
	res, err, _ := b.sg.Do(epoch.String(), func() (any, error) {
		if b, ok := b.cache[epoch]; ok {
			return b, nil
		}
		beacon, err := b.svc.Beacon(ctx, epoch)
		if err == nil {
			b.cache[epoch] = beacon
			// evict old beacons, keep last 4
			// this works properly under assumption that beacons are generally asked
			// in order, and that we don't skip epochs
			if epoch > 4 {
				delete(b.cache, epoch-4)
			}

		}
		return beacon, err
	})
	return res.(types.Beacon), err
}
