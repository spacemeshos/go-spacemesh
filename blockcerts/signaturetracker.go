package blockcerts

import (
    "context"
    "fmt"
    "sync"

    certtypes "github.com/spacemeshos/go-spacemesh/blockcerts/types"
    "github.com/spacemeshos/go-spacemesh/common/types"
    "github.com/spacemeshos/go-spacemesh/log"
)

type SigCacher interface {
    CacheBlockSignature(ctx context.Context,
        blockSignature certtypes.BlockSignatureMsg)
}

type sigCache struct {
    logger             log.Logger
    blockSigsByLayer   sync.Map // entries are written once but read many times
    cacheBoundary      types.LayerID
    cacheBoundaryMutex sync.RWMutex
    signaturesRequired int
    completedCertsCh   chan<- certtypes.BlockCertificate
}

func (c *sigCache) CacheBlockSignature(ctx context.Context, sigMsg certtypes.BlockSignatureMsg) {
    // TODO: decide on how to make this concurrent without asking the use to manage it
    logger := c.logger.WithContext(ctx)
    c.cacheBoundaryMutex.RLock()
    logger.Debug("CacheBlockSignature: acquired Rlock on the cache boundary")
    defer c.cacheBoundaryMutex.RUnlock()
    if sigMsg.LayerID.Before(c.cacheBoundary) {
        logger.Error("block msg is from layer %d which is older "+
            "than cache boundary %d", sigMsg.LayerID.Value, c.cacheBoundary.Value)
        return
    }
    tracker, loaded := c.blockSigsByLayer.LoadOrStore(sigMsg.LayerID.Value,
        newBlockSignatureTracker(sigMsg.LayerID, c.completedCertsCh, c.signaturesRequired, logger)) // TODO: fix memory issue (newBlockSig... called every time even if loading)
    if !loaded {
        logger.Debug("newBlockSignatureTracker: created tracker for block "+
            "signatures for layer %v", sigMsg.LayerID.String())
    }
    cert := tracker.(*blockSignatureTracker)
    cert.addSig(sigMsg)
}

// updateCacheBoundary updates the layer boundary. Block signatures aren't
// cached from before boundary. The cache limit should follow hdist.
func (c *sigCache) updateCacheBoundary(layer types.LayerID) error {
    c.cacheBoundaryMutex.Lock()
    defer c.cacheBoundaryMutex.Unlock()
    // The layer boundary for the cache should only move forward.
    if layer.Value <= c.cacheBoundary.Value {
        err := fmt.Errorf("new trackableBlockSignature cache limit is equal to or " +
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

type trackableBlockSignature struct {
    BlockID types.BlockID
    certtypes.BlockSignature
}
type blockSignatureTracker struct {
    layerID        types.LayerID
    signatures     map[types.NodeID]trackableBlockSignature
    signatureCount map[types.BlockID]int
    sync.Mutex
    completedCerts     chan<- certtypes.BlockCertificate
    signaturesRequired int
    ifNotAlready       sync.Once // completedCerts channel only gets one cert
    logger             log.Logger
}

func newBlockSignatureTracker(layerID types.LayerID, completedCerts chan<- certtypes.BlockCertificate,
    signaturesRequired int, logger log.Logger) *blockSignatureTracker {
    newTracker := blockSignatureTracker{
        layerID:            layerID,
        signatures:         map[types.NodeID]trackableBlockSignature{},
        signatureCount:     map[types.BlockID]int{},
        signaturesRequired: signaturesRequired,
        Mutex:              sync.Mutex{},
        completedCerts:     completedCerts,
        ifNotAlready:       sync.Once{},
        logger:             logger,
    }

    return &newTracker
}

// addSig doesn't do any validation or verification and is an atomic blocking operation.
// It should be run in its own goroutine. // TODO: notify somehow if signature from node already exists
func (t *blockSignatureTracker) addSig(signature certtypes.BlockSignatureMsg) {
    t.logger.Debug("addSig entry")
    t.Lock()
    t.logger.Debug("addSig: acquired lock on self (sigTracker)")
    defer t.Unlock()
    defer t.logger.Debug("addSig: released lock on self")
    // add signature and increase count
    t.signatures[signature.SignerNodeID] = trackableBlockSignature{
        BlockID:        signature.BlockID,
        BlockSignature: signature.BlockSignature,
    }
    t.signatureCount[signature.BlockID] += int(signature.SignerCommitteeSeats)

    if t.signatureCount[signature.BlockID] == t.signaturesRequired {
        var sigs = make([]certtypes.BlockSignature, 0, t.signaturesRequired)
        for _, sig := range t.signatures {
            isMajorityBlockID := sig.BlockID.String() == signature.BlockID.String()
            if isMajorityBlockID {
                sigs = append(sigs, sig.BlockSignature)
            }
        }
        blockCert := certtypes.BlockCertificate{
            BlockID:               signature.BlockID,
            LayerID:               t.layerID,
            TerminationSignatures: sigs,
        }
        t.logger.Debug("addSig: about to add completed cert to channel")
        t.completedCerts <- blockCert
        t.logger.Debug("addSig: added certified block to channel")
    }
    return
}
