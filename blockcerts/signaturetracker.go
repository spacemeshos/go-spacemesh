package blockcerts

import (
	"context"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type SigCacher interface {
	CacheBlockSignature(ctx context.Context,
		layerID types.LayerID, signature BlockSignature)
}

type sigCache struct {
	logger             log.Logger
	blockSigsByLayer   sync.Map // entries are written once but read many times
	cacheBoundary      types.LayerID
	cacheBoundaryMutex sync.RWMutex
	completedCertsCh   chan<- BlockCertificate
}

func (c *sigCache) CacheBlockSignature(ctx context.Context,
	layerID types.LayerID, signature BlockSignature) {
	logger := c.logger.WithContext(ctx)
	c.cacheBoundaryMutex.RLock()
	defer c.cacheBoundaryMutex.RUnlock()
	if layerID.Value < c.cacheBoundary.Value {
		logger.Error("block msg is from layer %d which is older "+
			"than cache boundary %d", layerID.Value, c.cacheBoundary.Value)
		return
	}
	tracker, _ := c.blockSigsByLayer.LoadOrStore(layerID.Value, blockSignatureTracker{})
	cert := tracker.(*blockSignatureTracker)
	go cert.addSig(signature) // queue up the msg and return
	// ain't nobody got time for safe memory writes
}

// updateCacheBoundary updates the layer boundary. Block signatures aren't
// cached from before boundary. The cache limit should follow hdist.
func (c *sigCache) updateCacheBoundary(layer types.LayerID) error {
	c.cacheBoundaryMutex.Lock()
	defer c.cacheBoundaryMutex.Unlock()
	// The layer boundary for the cache should only move forward.
	if layer.Value <= c.cacheBoundary.Value {
		err := fmt.Errorf("new BlockSignature cache limit is equal to or " +
			"older than previous cacheBoundary")
		c.logger.Error(err.Error())
		return err
	}
	go c.cleanup(c.cacheBoundary) // cleanup starting from old cacheBoundary
	c.cacheBoundary = layer
	return nil
}

func (c *sigCache) cleanup(startingAt types.LayerID) {
	start := startingAt.Value
	c.cacheBoundaryMutex.RLock()
	end := c.cacheBoundary.Value
	c.cacheBoundaryMutex.RUnlock()
	for layer := start; layer < end; layer++ {
		c.blockSigsByLayer.Delete(layer)
	}
}

type blockSignatureTracker struct {
	layerID        types.LayerID
	signatures     map[types.NodeID]BlockSignature
	thresholdCount map[types.BlockID]int
	sync.Mutex
	completedCerts chan<- BlockCertificate
	ifNotAlready   sync.Once // completedCerts channel only gets one cert
	logger         log.Logger
}

// addSig doesn't do any validation or verification and is an atomic operation.
// It should be run in its own goroutine.
func (t *blockSignatureTracker) addSig(sig BlockSignature) {
	t.Lock()
	defer t.Unlock()
	// check if a blockID reached threshold # of signatures
	var majorityBlockID types.BlockID
	var thresholdReached bool
	for blockID, threshold := range t.thresholdCount {
		if len(t.signatures) == threshold {
			majorityBlockID = blockID
			thresholdReached = true
			break
		} else if len(t.signatures) > threshold {
			panic("mutex locking bad") // TODO: log error instead of panic
		}
	}
	if thresholdReached {
		t.ifNotAlready.Do(func() {
			var sigs []BlockCertificateSignature
			for _, sig := range t.signatures {
				if sig.blockID.Compare(majorityBlockID) {
					sigs = append(sigs, sig.BlockCertificateSignature)
				}
			}
			blockCert := BlockCertificate{
				BlockID:               majorityBlockID,
				terminationSignatures: sigs,
			}
			t.completedCerts <- blockCert
		})
		return
	}

	t.signatures[sig.signerNodeID] = sig
	t.thresholdCount[sig.blockID] += int(sig.signerCommitteeSeats)
}
