package blockcerts

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
)

type SigCacher interface {
	CacheBlockSignature(signature BlockSignature)
}

type thinBlockSignature struct {
}

type blockSignatureTracker struct {
	signatures map[uint32]thinBlockSignature
	threshold  int
	sync.Mutex
	sync.Once
}

func (t *blockSignatureTracker) addSig(sig BlockSignature) {
	t.Lock()
	defer t.Unlock()
	if len(t.signatures) > t.threshold {
		return
	}

	// continue with this (need a hash for the signature map keys)
}

type sigCache struct {
	logger             log.Logger
	blockSigsByLayer   sync.Map // entries are written once but read many times
	cacheBoundary      types.LayerID
	cacheBoundaryMutex sync.RWMutex
	completedCerts     chan<- BlockCertificate
}

func (c *sigCache) CacheBlockSignature(ctx context.Context, signature BlockSignature) {
	logger := c.logger.WithContext(ctx)
	layerID := signature.layerID
	c.cacheBoundaryMutex.RLock()
	defer c.cacheBoundaryMutex.RUnlock()
	if layerID.Value < c.cacheBoundary.Value {
		logger.Error("block signature is from layer %d which is older "+
			"than cache boundary %d", layerID.Value, c.cacheBoundary.Value)
		return
	}
	tracker, _ := c.blockSigsByLayer.LoadOrStore(layerID.Value, blockSignatureTracker{})
	cert := tracker.(*blockSignatureTracker)
	go cert.addSig(signature) // queue up the signature and return
	// cuz nobody got time safe memory writes
}

// updateCacheBoundary updates the layer boundary before which block signatures
// should not be cached. The cache limit should follow hdist.
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
