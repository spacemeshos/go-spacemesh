package multipeer

import (
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

type dumbSet struct {
	rangesync.OrderedSet
}

var _ OrderedSet = &dumbSet{}

// NewDumbHashSet creates an unoptimized OrderedSet to be used for testing purposes.
// If disableReAdd is true, receiving the same item multiple times will fail.
func NewDumbHashSet(disableReAdd bool) OrderedSet {
	return &dumbSet{rangesync.NewDumbSet(disableReAdd)}
}

func (ds *dumbSet) EnsureLoaded() error {
	return nil
}

func (ds *dumbSet) Advance() error {
	return nil
}

func (ds *dumbSet) Received() rangesync.SeqResult {
	return ds.Items()
}

func (ds *dumbSet) Has(k rangesync.KeyBytes) (bool, error) {
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

func (ds *dumbSet) Copy(syncScope bool) rangesync.OrderedSet {
	return &dumbSet{ds.OrderedSet.Copy(syncScope)}
}

func (ds *dumbSet) Release() error {
	return nil
}
