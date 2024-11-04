package multipeer

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/p2p"
)

type (
	SyncRunner = syncRunner
	SplitSync  = splitSync
)

var (
	GetDelimiters                  = getDelimiters
	NewSyncQueue                   = newSyncQueue
	NewSplitSync                   = newSplitSync
	NewSyncList                    = newSyncList
	NewMultiPeerReconcilerInternal = newMultiPeerReconciler
)

func (mpr *MultiPeerReconciler) FullSync(ctx context.Context, syncPeers []p2p.Peer) error {
	return mpr.fullSync(ctx, syncPeers)
}
