package multipeer

import (
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// DumbSet is an unoptimized OrderedSet to be used for testing purposes.
// It builds on rangesync.DumbSet.
type DumbSet struct {
	*rangesync.DumbSet
}

var _ OrderedSet = &DumbSet{}

// NewDumbHashSet creates an unoptimized OrderedSet to be used for testing purposes.
// If disableReAdd is true, receiving the same item multiple times will fail.
func NewDumbHashSet() *DumbSet {
	return &DumbSet{
		DumbSet: &rangesync.DumbSet{},
	}
}

// Advance implements OrderedSet.
func (ds *DumbSet) EnsureLoaded() error {
	return nil
}

// Advance implements OrderedSet.
func (ds *DumbSet) Advance() error {
	return nil
}

// Has implements OrderedSet.
func (ds *DumbSet) Has(k rangesync.KeyBytes) (bool, error) {
	var first rangesync.KeyBytes
	sr := ds.Items()
	for cur := range sr.Seq {
		if first == nil {
			first = cur
		} else if first.Compare(cur) == 0 {
			return false, sr.Error()
		}
		if k.Compare(cur) == 0 {
			return true, sr.Error()
		}
	}
	return false, sr.Error()
}

// Copy implements OrderedSet.
func (ds *DumbSet) Copy(syncScope bool) rangesync.OrderedSet {
	return &DumbSet{ds.DumbSet.Copy(syncScope).(*rangesync.DumbSet)}
}

// Release implements OrderedSet.
func (ds *DumbSet) Release() error {
	return nil
}
