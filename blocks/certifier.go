package blocks

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errMultipleCerts      = errors.New("multiple valid certificates")
	errInvalidCert        = errors.New("invalid certificate")
	errInvalidCertMsg     = errors.New("invalid cert msg")
	errUnexpectedMsg      = errors.New("unexpected lid")
	errBeaconNotAvailable = errors.New("beacon not available")
)

// CertConfig is the config for Certifier.
type CertConfig struct {
	CommitteeSize    int
	CertifyThreshold int
	LayerBuffer      uint32
	NumLayersToKeep  uint32
}

func defaultCertConfig() CertConfig {
	return CertConfig{
		CommitteeSize:    10,
		CertifyThreshold: 6,
		LayerBuffer:      5,
		NumLayersToKeep:  10,
	}
}

// CertifierOpt for configuring Certifier.
type CertifierOpt func(*Certifier)

// WithCertContext modifies parent context for Certifier.
func WithCertContext(ctx context.Context) CertifierOpt {
	return func(c *Certifier) {
		c.ctx = ctx
	}
}

// WithCertConfig defines cfg for Certifier.
func WithCertConfig(cfg CertConfig) CertifierOpt {
	return func(c *Certifier) {
		c.cfg = cfg
	}
}

// WithCertifierLogger defines logger for Certifier.
func WithCertifierLogger(logger log.Log) CertifierOpt {
	return func(c *Certifier) {
		c.logger = logger
	}
}

func withNonceFetcher(nf nonceFetcher) CertifierOpt {
	return func(c *Certifier) {
		c.nonceFetcher = nf
	}
}

type defaultFetcher struct {
	cdb *datastore.CachedDB
}

func (f defaultFetcher) VRFNonce(nodeID types.NodeID, epoch types.EpochID) (types.VRFPostIndex, error) {
	nonce, err := f.cdb.VRFNonce(nodeID, epoch)
	if err != nil {
		return types.VRFPostIndex(0), fmt.Errorf("get vrf nonce: %w", err)
	}
	return nonce, nil
}

type certInfo struct {
	registered, done bool
	totalEligibility uint16
	signatures       []types.CertifyMessage
}

// Certifier collects enough CertifyMessage for a given hare output and generate certificate.
type Certifier struct {
	logger log.Log
	cfg    CertConfig
	once   sync.Once
	eg     errgroup.Group
	ctx    context.Context
	cancel func()

	db           *datastore.CachedDB
	oracle       hare.Rolacle
	nodeID       types.NodeID
	signer       *signing.EdSigner
	nonceFetcher nonceFetcher
	edVerifier   *signing.EdVerifier
	publisher    pubsub.Publisher
	layerClock   layerClock
	beacon       system.BeaconGetter
	tortoise     system.Tortoise

	mu          sync.Mutex
	certifyMsgs map[types.LayerID]map[types.BlockID]*certInfo
	certCount   map[types.EpochID]int

	collector *collector
}

// NewCertifier creates new block certifier.
func NewCertifier(
	db *datastore.CachedDB,
	o hare.Rolacle,
	n types.NodeID,
	s *signing.EdSigner,
	v *signing.EdVerifier,
	p pubsub.Publisher,
	lc layerClock,
	b system.BeaconGetter,
	tortoise system.Tortoise,
	opts ...CertifierOpt,
) *Certifier {
	c := &Certifier{
		logger:      log.NewNop(),
		cfg:         defaultCertConfig(),
		ctx:         context.Background(),
		db:          db,
		oracle:      o,
		nodeID:      n,
		signer:      s,
		edVerifier:  v,
		publisher:   p,
		layerClock:  lc,
		beacon:      b,
		tortoise:    tortoise,
		certifyMsgs: make(map[types.LayerID]map[types.BlockID]*certInfo),
		certCount:   map[types.EpochID]int{},
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.nonceFetcher == nil {
		c.nonceFetcher = defaultFetcher{cdb: db}
	}
	c.collector = newCollector(c)

	c.ctx, c.cancel = context.WithCancel(c.ctx)
	return c
}

// Start starts the background goroutine for periodic pruning.
func (c *Certifier) Start() {
	c.once.Do(func() {
		c.eg.Go(func() error {
			return c.run()
		})
	})
}

// Stop stops the outstanding goroutines.
func (c *Certifier) Stop() {
	c.cancel()
	err := c.eg.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		c.logger.With().Error("blockGen task failure", log.Err(err))
	}
}

func (c *Certifier) isShuttingDown() bool {
	select {
	case <-c.ctx.Done():
		return true
	default:
		return false
	}
}

func (c *Certifier) run() error {
	for layer := c.layerClock.CurrentLayer(); ; layer = layer.Add(1) {
		select {
		case <-c.layerClock.AwaitLayer(layer):
			c.prune()
		case <-c.ctx.Done():
			return fmt.Errorf("context done: %w", c.ctx.Err())
		}
	}
}

func (c *Certifier) prune() {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := types.GetEffectiveGenesis()
	current := c.layerClock.CurrentLayer()
	if current.Uint32() > c.cfg.NumLayersToKeep {
		cutoff = current.Sub(c.cfg.NumLayersToKeep)
	}
	for lid := range c.certifyMsgs {
		if lid.Before(cutoff) {
			c.logger.With().Debug("removing layer from cert registration", lid)
			delete(c.certifyMsgs, lid)
		}
	}
}

func (c *Certifier) createIfNeeded(lid types.LayerID, bid types.BlockID) {
	if _, ok := c.certifyMsgs[lid]; !ok {
		c.certifyMsgs[lid] = make(map[types.BlockID]*certInfo)
	}
	if _, ok := c.certifyMsgs[lid][bid]; !ok {
		c.certifyMsgs[lid][bid] = &certInfo{
			signatures: make([]types.CertifyMessage, 0, c.cfg.CommitteeSize),
		}
	}
}

// RegisterForCert register to generate a certificate for the specified layer/block.
func (c *Certifier) RegisterForCert(ctx context.Context, lid types.LayerID, bid types.BlockID) error {
	logger := c.logger.WithContext(ctx).WithFields(lid, bid)
	logger.Debug("certifier registered")

	c.mu.Lock()
	defer c.mu.Unlock()
	c.createIfNeeded(lid, bid)
	c.certifyMsgs[lid][bid].registered = true
	return c.tryGenCert(ctx, logger, lid, bid)
}

// CertifyIfEligible signs the hare output, along with its role proof as a certifier, and gossip the CertifyMessage
// if the node is eligible to be a certifier.
func (c *Certifier) CertifyIfEligible(ctx context.Context, logger log.Log, lid types.LayerID, bid types.BlockID) error {
	if _, err := c.beacon.GetBeacon(lid.GetEpoch()); err != nil {
		return errBeaconNotAvailable
	}
	nonce, err := c.nonceFetcher.VRFNonce(c.nodeID, lid.GetEpoch())
	if err != nil { // never submitted an atx, not eligible
		if errors.Is(err, sql.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("failed to get own vrf nonce: %w", err)
	}

	// check if the node is eligible to certify the hare output
	proof, err := c.oracle.Proof(ctx, nonce, lid, eligibility.CertifyRound)
	if err != nil {
		logger.With().Error("failed to get eligibility proof to certify", log.Err(err))
		return err
	}

	eligibilityCount, err := c.oracle.CalcEligibility(ctx, lid, eligibility.CertifyRound, c.cfg.CommitteeSize, c.nodeID, nonce, proof)
	if err != nil {
		return err
	}
	if eligibilityCount == 0 { // not eligible
		return nil
	}

	msg := types.CertifyMessage{
		CertifyContent: types.CertifyContent{
			LayerID:        lid,
			BlockID:        bid,
			EligibilityCnt: eligibilityCount,
			Proof:          proof,
		},
		SmesherID: c.nodeID,
	}
	msg.Signature = c.signer.Sign(signing.HARE, msg.Bytes())
	data, err := codec.Encode(&msg)
	if err != nil {
		logger.With().Panic("failed to serialize certify message", log.Err(err))
		return err
	}
	if err = c.publisher.Publish(ctx, pubsub.BlockCertify, data); err != nil {
		logger.With().Error("failed to send certify message", log.Err(err))
		return err
	}
	return nil
}

// NumCached returns the number of layers being cached in memory.
func (c *Certifier) NumCached() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.certifyMsgs)
}

// HandleSyncedCertificate handles Certificate from sync.
func (c *Certifier) HandleSyncedCertificate(ctx context.Context, lid types.LayerID, cert *types.Certificate) error {
	logger := c.logger.WithContext(ctx).WithFields(lid, cert.BlockID)
	logger.Debug("processing synced certificate")
	if err := c.validateCert(ctx, logger, cert); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.checkAndSave(ctx, logger, lid, cert); err != nil {
		return err
	}
	return nil
}

func (c *Certifier) validateCert(ctx context.Context, logger log.Log, cert *types.Certificate) error {
	eligibilityCnt := uint16(0)
	for _, msg := range cert.Signatures {
		if err := c.validate(ctx, logger, msg); err != nil {
			continue
		}
		eligibilityCnt += msg.EligibilityCnt
	}
	if int(eligibilityCnt) < c.cfg.CertifyThreshold {
		logger.With().Warning("certificate not meeting threshold",
			log.Int("num_msgs", len(cert.Signatures)),
			log.Int("threshold", c.cfg.CertifyThreshold),
			log.Uint16("eligibility_count", eligibilityCnt),
		)
		return errInvalidCert
	}
	return nil
}

func (c *Certifier) certified(lid types.LayerID, bid types.BlockID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.certifyMsgs[lid]; ok {
		if _, ok := c.certifyMsgs[lid][bid]; ok {
			return c.certifyMsgs[lid][bid].done
		}
	}
	return false
}

func (c *Certifier) expected(lid types.LayerID) bool {
	current := c.layerClock.CurrentLayer()
	start := types.GetEffectiveGenesis()
	if current.Uint32() > c.cfg.LayerBuffer+1 {
		start = current.Sub(c.cfg.LayerBuffer + 1)
	}
	// only accept early msgs within a range and with limited size to prevent DOS
	return !lid.Before(start) && !lid.After(current.Add(c.cfg.LayerBuffer))
}

func (c *Certifier) HandleCertifyMessage(ctx context.Context, peer p2p.Peer, data []byte) error {
	err := c.handleCertifyMessage(ctx, peer, data)
	if err != nil && errors.Is(err, errMalformedData) {
		c.logger.WithContext(ctx).With().Warning("malformed cert msg", log.Stringer("peer", peer), log.Err(err))
	}
	return err
}

// HandleCertifyMessage is the gossip receiver for certify message.
func (c *Certifier) handleCertifyMessage(ctx context.Context, peer p2p.Peer, data []byte) error {
	if c.isShuttingDown() {
		return errors.New("certifier shutting down")
	}

	logger := c.logger.WithContext(ctx)
	var msg types.CertifyMessage
	if err := codec.Decode(data, &msg); err != nil {
		return errMalformedData
	}

	lid := msg.LayerID
	bid := msg.BlockID
	logger = logger.WithFields(lid, bid)

	if _, err := c.beacon.GetBeacon(lid.GetEpoch()); err != nil {
		return errBeaconNotAvailable
	}

	if !c.expected(lid) {
		logger.With().Debug("received message for unexpected layer", lid)
		return errUnexpectedMsg
	}

	if c.certified(lid, bid) {
		// should still gossip this msg to peers even when this node has created a certificate
		return nil
	}

	if err := c.validate(ctx, logger, msg); err != nil {
		return err
	}

	if err := c.saveMessage(ctx, logger, msg); err != nil {
		return err
	}
	return nil
}

func (c *Certifier) validate(ctx context.Context, logger log.Log, msg types.CertifyMessage) error {
	if !c.edVerifier.Verify(signing.HARE, msg.SmesherID, msg.Bytes(), msg.Signature) {
		return fmt.Errorf("%w: failed to verify signature", errMalformedData)
	}
	valid, err := c.oracle.Validate(ctx, msg.LayerID, eligibility.CertifyRound, c.cfg.CommitteeSize, msg.SmesherID, msg.Proof, msg.EligibilityCnt)
	if err != nil {
		logger.With().Warning("failed to validate cert msg", log.Err(err))
		return err
	}
	if !valid {
		logger.With().Warning("oracle deemed cert msg invalid", log.Stringer("smesher", msg.SmesherID))
		return errInvalidCertMsg
	}
	return nil
}

func (c *Certifier) saveMessage(ctx context.Context, logger log.Log, msg types.CertifyMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	lid := msg.LayerID
	bid := msg.BlockID
	c.createIfNeeded(lid, bid)

	c.certifyMsgs[lid][bid].signatures = append(c.certifyMsgs[lid][bid].signatures, msg)
	c.certifyMsgs[lid][bid].totalEligibility += msg.EligibilityCnt
	logger.With().Debug("saved certify msg",
		log.Uint16("eligibility_count", c.certifyMsgs[lid][bid].totalEligibility),
		log.Int("num_msg", len(c.certifyMsgs[lid][bid].signatures)),
	)

	if c.certifyMsgs[lid][bid].registered {
		return c.tryGenCert(ctx, logger, lid, bid)
	}
	return nil
}

func (c *Certifier) tryGenCert(ctx context.Context, logger log.Log, lid types.LayerID, bid types.BlockID) error {
	if _, ok := c.certifyMsgs[lid]; !ok {
		logger.Fatal("missing layer in cache")
	}
	if _, ok := c.certifyMsgs[lid][bid]; !ok {
		logger.Fatal("missing block in cache")
	}

	if c.certifyMsgs[lid][bid].done ||
		c.certifyMsgs[lid][bid].totalEligibility < uint16(c.cfg.CertifyThreshold) {
		return nil
	}

	if !c.certifyMsgs[lid][bid].registered {
		// do not try to generate a certificate for this block.
		// wait for syncer to download from peers
		return nil
	}

	logger.With().Info("generating certificate",
		log.Uint16("eligibility_count", c.certifyMsgs[lid][bid].totalEligibility),
		log.Int("num_msg", len(c.certifyMsgs[lid][bid].signatures)),
	)
	cert := &types.Certificate{
		BlockID:    bid,
		Signatures: c.certifyMsgs[lid][bid].signatures,
	}
	if err := c.checkAndSave(ctx, logger, lid, cert); err != nil {
		return err
	}
	c.certifyMsgs[lid][bid].done = true
	return nil
}

func (c *Certifier) checkAndSave(ctx context.Context, logger log.Log, lid types.LayerID, cert *types.Certificate) error {
	oldCerts, err := certificates.Get(c.db, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return err
	}

	logger.With().Debug("found old certs", log.Int("num_cert", len(oldCerts)))
	var valid, invalid []types.BlockID
	for _, old := range oldCerts {
		if old.Block == cert.BlockID {
			// just verified
			continue
		}
		if old.Cert == nil {
			// the original live hare output should be trumped by a valid certificate
			logger.With().Warning("original hare output trumped", log.Stringer("hare_output", old.Block))
			invalid = append(invalid, old.Block)
			continue
		}
		if err = c.validateCert(ctx, logger, old.Cert); err == nil {
			logger.With().Warning("old cert still valid", log.Stringer("old_cert", old.Block))
			valid = append(valid, old.Block)
		} else {
			logger.With().Debug("old cert not valid", log.Stringer("old_cert", old.Block))
			invalid = append(invalid, old.Block)
		}
	}
	if err = c.save(ctx, lid, cert, valid, invalid); err != nil {
		return err
	}
	if len(valid) > 0 {
		logger.Warning("multiple valid certificates found")
		// stop processing certify message for this block
		if _, ok := c.certifyMsgs[lid]; ok {
			if _, ok = c.certifyMsgs[lid][cert.BlockID]; ok {
				c.certifyMsgs[lid][cert.BlockID].done = true
			}
		}
		c.tortoise.OnHareOutput(lid, types.EmptyBlockID)
		return errMultipleCerts
	}
	c.addCertCount(lid.GetEpoch())
	c.tortoise.OnHareOutput(lid, cert.BlockID)
	return nil
}

func (c *Certifier) addCertCount(epoch types.EpochID) {
	c.certCount[epoch]++
	delete(c.certCount, epoch-2)
}

func (c *Certifier) CertCount() map[types.EpochID]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := map[types.EpochID]int{}
	for epoch, count := range c.certCount {
		result[epoch] = count
	}
	return result
}

func (c *Certifier) save(ctx context.Context, lid types.LayerID, cert *types.Certificate, valid, invalid []types.BlockID) error {
	if len(valid)+len(invalid) == 0 {
		return certificates.Add(c.db, lid, cert)
	}
	return c.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		if err := certificates.Add(dbtx, lid, cert); err != nil {
			return err
		}
		for _, bid := range valid {
			if err := certificates.SetValid(dbtx, lid, bid); err != nil {
				return err
			}
		}
		for _, bid := range invalid {
			if err := certificates.SetInvalid(dbtx, lid, bid); err != nil {
				return err
			}
		}
		return nil
	})
}
