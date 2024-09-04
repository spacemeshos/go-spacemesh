package dbsync

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type idStore interface {
	clone() idStore
	registerHash(h types.KeyBytes) error
	all(ctx context.Context) (types.Seq, error)
	from(ctx context.Context, from types.KeyBytes) (types.Seq, error)
}
