package types

import "github.com/spacemeshos/go-spacemesh/common/types"

// HashDataPromiseResult is the result strict when requesting Data corresponding to the Hash.
type HashDataPromiseResult struct {
	Err     error
	Hash    types.Hash32
	Data    []byte
	IsLocal bool
}
