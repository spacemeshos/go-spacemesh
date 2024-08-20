package blocks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
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
	CommitteeSize    int `mapstructure:"committee-size"`
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

// WithCertConfig defines cfg for Certifier.
func WithCertConfig(cfg CertConfig) CertifierOpt {
	return func(c *Certifier) {
		c.cfg = cfg
	}
}

// WithCertifierLogger defines logger for Certifier.
func WithCertifierLogger(logger *zap.Logger) CertifierOpt {
	return func(c *Certifier) {
		c.logger = logger
	}
}

type certInfo struct {
	registered, done bool
	totalEligibility uint16
	signatures       []types.CertifyMessage
}

// Certifier collects enough CertifyMessage for a given hare output and generate certificate.
type Certifier struct {
	logger *zap.Logger
	cfg    CertConfig
	once   sync.Once
	eg     errgroup.Group

	stop    func()
	stopped atomic.Bool

	db         sql.StateDatabase
	oracle     eligibility.Rolacle
	signers    map[types.NodeID]*signing.EdSigner
	edVerifier *signing.EdVerifier
	publisher  pubsub.Publisher
	layerClock layerClock
	beacon     system.BeaconGetter
	tortoise   system.Tortoise

	mu          sync.Mutex
	certifyMsgs map[types.LayerID]map[types.BlockID]*certInfo
	certCount   map[types.EpochID]int

	collector *collector
}

// NewCertifier creates new block certifier.
func NewCertifier(
	db sql.StateDatabase,
	o eligibility.Rolacle,

	v *signing.EdVerifier,
	p pubsub.Publisher,
	lc layerClock,
	b system.BeaconGetter,
	tortoise system.Tortoise,
	opts ...CertifierOpt,
) *Certifier {
	c := &Certifier{
		logger:      zap.NewNop(),
		cfg:         defaultCertConfig(),
		db:          db,
		oracle:      o,
		signers:     make(map[types.NodeID]*signing.EdSigner),
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
	c.collector = newCollector(c)

	return c
}

func (c *Certifier) Register(sig *signing.EdSigner) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.signers[sig.NodeID()]; exists {
		c.logger.Error("signing key already registered", log.ZShortStringer("id", sig.NodeID()))
		return
	}

	c.logger.Info("registered signing key", log.ZShortStringer("id", sig.NodeID()))
	c.signers[sig.NodeID()] = sig
}

// Start starts the background goroutine for periodic pruning.
func (c *Certifier) Start(ctx context.Context) {
	c.once.Do(func() {
		ctx, c.stop = context.WithCancel(ctx)
		c.eg.Go(func() error {
			return c.run(ctx)
		})
	})
}

// Stop stops the outstanding goroutines.
func (c *Certifier) Stop() {
	c.stopped.Store(true)
	if c.stop == nil {
		return // not started
	}
	c.stop()
	err := c.eg.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		c.logger.Error("certifier task failure", zap.Error(err))
	}
}

func (c *Certifier) run(ctx context.Context) error {
	for layer := c.layerClock.CurrentLayer(); ; layer = layer.Add(1) {
		select {
		case <-c.layerClock.AwaitLayer(layer):
			c.prune()
		case <-ctx.Done():
			return fmt.Errorf("context done: %w", ctx.Err())
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
			c.logger.Debug("removing layer from cert registration", zap.Stringer("layer_id", lid))
			delete(c.certifyMsgs, lid)
		}
	}
}

func (c *Certifier) createIfNeeded(lid types.LayerID, bid types.BlockID) *certInfo {
	if _, ok := c.certifyMsgs[lid]; !ok {
		c.certifyMsgs[lid] = make(map[types.BlockID]*certInfo)
	}
	if _, ok := c.certifyMsgs[lid][bid]; !ok {
		c.certifyMsgs[lid][bid] = &certInfo{
			signatures: make([]types.CertifyMessage, 0, c.cfg.CommitteeSize),
		}
	}
	return c.certifyMsgs[lid][bid]
}

// RegisterForCert register to generate a certificate for the specified layer/block.
func (c *Certifier) RegisterForCert(ctx context.Context, lid types.LayerID, bid types.BlockID) error {
	logger := c.logger.With(
		log.ZContext(ctx),
		zap.Stringer("layer_id", lid),
		zap.Stringer("block_id", bid),
	)
	logger.Debug("certifier registered")

	c.mu.Lock()
	defer c.mu.Unlock()
	info := c.createIfNeeded(lid, bid)
	info.registered = true
	return c.tryGenCert(ctx, logger, lid, bid, info)
}

// CertifyIfEligible signs the hare output, along with its role proof as a certifier, and gossip the CertifyMessage
// if the node is eligible to be a certifier.
func (c *Certifier) CertifyIfEligible(ctx context.Context, lid types.LayerID, bid types.BlockID) error {
	beacon, err := c.beacon.GetBeacon(lid.GetEpoch())
	if err != nil {
		return errBeaconNotAvailable
	}

	c.mu.Lock()
	signers := maps.Values(c.signers)
	c.mu.Unlock()

	var errs error
	for _, s := range signers {
		if err := c.certifySingleSigner(ctx, s, lid, bid, beacon); err != nil {
			errs = errors.Join(
				errs,
				fmt.Errorf("certifying block %v/%v by %s: %w", lid, bid, s.NodeID().ShortString(), err),
			)
		}
	}
	return errs
}

func (c *Certifier) certifySingleSigner(
	ctx context.Context,
	s *signing.EdSigner,
	lid types.LayerID,
	bid types.BlockID,
	beacon types.Beacon,
) error {
	proof := eligibility.GenVRF(context.Background(), s.VRFSigner(), beacon, lid, eligibility.CertifyRound)
	eligibilityCount, err := c.oracle.CalcEligibility(
		ctx,
		lid,
		eligibility.CertifyRound,
		c.cfg.CommitteeSize,
		s.NodeID(),
		proof,
	)
	if err != nil {
		return fmt.Errorf("calculating eligibility: %w", err)
	}
	if eligibilityCount == 0 { // not eligible
		return nil
	}

	msg := newCertifyMsg(s, lid, bid, proof, eligibilityCount)
	if err = c.publisher.Publish(ctx, pubsub.BlockCertify, codec.MustEncode(msg)); err != nil {
		return fmt.Errorf("publishing block certification message: %w", err)
	}
	return nil
}

func newCertifyMsg(
	s *signing.EdSigner,
	lid types.LayerID,
	bid types.BlockID,
	proof types.VrfSignature,
	eligibility uint16,
) *types.CertifyMessage {
	msg := &types.CertifyMessage{
		CertifyContent: types.CertifyContent{
			LayerID:        lid,
			BlockID:        bid,
			EligibilityCnt: eligibility,
			Proof:          proof,
		},
		SmesherID: s.NodeID(),
	}
	msg.Signature = s.Sign(signing.HARE, msg.Bytes())
	return msg
}

// NumCached returns the number of layers being cached in memory.
func (c *Certifier) NumCached() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.certifyMsgs)
}

// HandleSyncedCertificate handles Certificate from sync.
func (c *Certifier) HandleSyncedCertificate(ctx context.Context, lid types.LayerID, cert *types.Certificate) error {
	logger := c.logger.With(
		log.ZContext(ctx),
		zap.Stringer("layer_id", lid),
		zap.Stringer("block_id", cert.BlockID),
	)
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

func (c *Certifier) validateCert(ctx context.Context, logger *zap.Logger, cert *types.Certificate) error {
	eligibilityCnt := uint16(0)
	for _, msg := range cert.Signatures {
		if err := c.validate(ctx, logger, msg); err != nil {
			continue
		}
		eligibilityCnt += msg.EligibilityCnt
	}
	if int(eligibilityCnt) < c.cfg.CertifyThreshold {
		logger.Warn("certificate not meeting threshold",
			zap.Int("num_msgs", len(cert.Signatures)),
			zap.Int("threshold", c.cfg.CertifyThreshold),
			zap.Uint16("eligibility_count", eligibilityCnt),
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
		c.logger.Warn("malformed cert msg",
			log.ZContext(ctx),
			zap.Stringer("peer", peer),
			zap.Error(err),
		)
	}
	return err
}

// HandleCertifyMessage is the gossip receiver for certify message.
func (c *Certifier) handleCertifyMessage(ctx context.Context, _ p2p.Peer, data []byte) error {
	if c.stopped.Load() {
		return errors.New("certifier shutting down")
	}

	var msg types.CertifyMessage
	if err := codec.Decode(data, &msg); err != nil {
		return errMalformedData
	}

	lid := msg.LayerID
	bid := msg.BlockID
	logger := c.logger.With(
		log.ZContext(ctx),
		zap.Stringer("layer_id", lid),
		zap.Stringer("block_id", bid),
	)

	if _, err := c.beacon.GetBeacon(lid.GetEpoch()); err != nil {
		return errBeaconNotAvailable
	}

	if !c.expected(lid) {
		logger.Debug("received message for unexpected layer",
			zap.Stringer("layer_id", lid),
		)
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

func (c *Certifier) validate(ctx context.Context, logger *zap.Logger, msg types.CertifyMessage) error {
	if !c.edVerifier.Verify(signing.HARE, msg.SmesherID, msg.Bytes(), msg.Signature) {
		return fmt.Errorf("%w: failed to verify signature", errMalformedData)
	}
	valid, err := c.oracle.Validate(
		ctx,
		msg.LayerID,
		eligibility.CertifyRound,
		c.cfg.CommitteeSize,
		msg.SmesherID,
		msg.Proof,
		msg.EligibilityCnt,
	)
	if err != nil {
		logger.Warn("failed to validate cert msg", zap.Error(err))
		return err
	}
	if !valid {
		logger.Warn("oracle deemed cert msg invalid", zap.Stringer("smesher", msg.SmesherID))
		return errInvalidCertMsg
	}
	return nil
}

func (c *Certifier) saveMessage(ctx context.Context, logger *zap.Logger, msg types.CertifyMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	lid := msg.LayerID
	bid := msg.BlockID
	info := c.createIfNeeded(lid, bid)

	info.signatures = append(info.signatures, msg)
	info.totalEligibility += msg.EligibilityCnt
	logger.Debug("saved certify msg",
		zap.Uint16("eligibility_count", info.totalEligibility),
		zap.Int("num_msg", len(info.signatures)),
	)

	if info.registered {
		return c.tryGenCert(ctx, logger, lid, bid, info)
	}
	return nil
}

func (c *Certifier) tryGenCert(
	ctx context.Context,
	logger *zap.Logger,
	lid types.LayerID,
	bid types.BlockID,
	info *certInfo,
) error {
	if info.done || info.totalEligibility < uint16(c.cfg.CertifyThreshold) {
		return nil
	}

	if !info.registered {
		// do not try to generate a certificate for this block.
		// wait for syncer to download from peers
		return nil
	}

	logger.Debug("generating certificate",
		zap.Uint16("eligibility_count", c.certifyMsgs[lid][bid].totalEligibility),
		zap.Int("num_msg", len(c.certifyMsgs[lid][bid].signatures)),
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

func (c *Certifier) checkAndSave(
	ctx context.Context,
	logger *zap.Logger,
	lid types.LayerID,
	cert *types.Certificate,
) error {
	oldCerts, err := certificates.Get(c.db, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return err
	}

	logger.Debug("found old certs", zap.Int("num_cert", len(oldCerts)))
	var valid, invalid []types.BlockID
	for _, old := range oldCerts {
		if old.Block == cert.BlockID {
			// just verified
			continue
		}
		if old.Cert == nil {
			// the original live hare output should be trumped by a valid certificate
			logger.Warn("original hare output trumped", zap.Stringer("hare_output", old.Block))
			invalid = append(invalid, old.Block)
			continue
		}
		if err = c.validateCert(ctx, logger, old.Cert); err == nil {
			logger.Warn("old cert still valid", zap.Stringer("old_cert", old.Block))
			valid = append(valid, old.Block)
		} else {
			logger.Debug("old cert not valid", zap.Stringer("old_cert", old.Block))
			invalid = append(invalid, old.Block)
		}
	}
	if err = c.save(ctx, lid, cert, valid, invalid); err != nil {
		return err
	}
	if len(valid) > 0 {
		logger.Warn("multiple valid certificates found")
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
	return nil
}

func (c *Certifier) addCertCount(epoch types.EpochID) {
	c.certCount[epoch]++
	delete(c.certCount, epoch-2)
}

func (c *Certifier) CertCount() map[types.EpochID]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.certCount)
}

func (c *Certifier) save(
	ctx context.Context,
	lid types.LayerID,
	cert *types.Certificate,
	valid, invalid []types.BlockID,
) error {
	if len(valid)+len(invalid) == 0 {
		return certificates.Add(c.db, lid, cert)
	}
	return c.db.WithTx(ctx, func(dbtx sql.Transaction) error {
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
