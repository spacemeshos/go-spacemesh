package fptree

import (
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
)

var ErrEasySplitFailed = errEasySplitFailed

func (ft *FPTree) FingerprintInternal(
	x, y rangesync.KeyBytes,
	limit int,
	needNext bool,
) (fpr FPResult, err error) {
	ft.np.lockRead()
	defer ft.np.unlockRead()
	return ft.fingerprintInterval(x, y, limit, needNext)
}

func (ft *FPTree) EasySplit(x, y rangesync.KeyBytes, limit int) (sr SplitResult, err error) {
	return ft.easySplit(x, y, limit)
}

func (ft *FPTree) PoolNodeCount() int {
	return ft.np.nodeCount()
}

func (ft *FPTree) IDStore() sqlstore.IDStore {
	return ft.idStore
}
