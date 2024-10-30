package multipeer

import (
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// QQQQQ: rm
type DumbSet = rangesync.DumbSet

// QQQQQ: rm
func NewDumbHashSet() *DumbSet {
	return &rangesync.DumbSet{}
}
