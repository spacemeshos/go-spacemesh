package rangesync

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

// CollectSetItems returns the list of items in the given set.
func CollectSetItems(ctx context.Context, os OrderedSet) (r []types.KeyBytes, err error) {
	items, err := os.Items(ctx)
	if err != nil {
		return nil, err
	}
	var first types.KeyBytes
	for v, err := range items {
		if err != nil {
			return nil, err
		}
		if first == nil {
			first = v
		} else if v.Compare(first) == 0 {
			break
		}
		r = append(r, v)
	}
	return r, nil
}
