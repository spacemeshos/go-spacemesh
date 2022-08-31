package blocks

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/ed25519"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/system"
)

const numEarlyLayers = uint32(1)

var (
	errInvalidCert        = errors.New("invalid certificate")
	errInvalidCertMsg     = errors.New("invalid cert msg")
	errUnexpectedMsg      = errors.New("unexpected lid/bid")
	errInternal           = errors.New("internal")
	errBeaconNotAvailable = errors.New("beacon not available")
)

// CertConfig is the config for Certifier.
type CertConfig struct {
	CommitteeSize    int
	CertifyThreshold int
	NumLayersToKeep  uint32
}

func defaultCertConfig() CertConfig {
	return CertConfig{
		CommitteeSize:    10,
		CertifyThreshold: 6,
		NumLayersToKeep:  5,
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

	db         *sql.Database
	oracle     hare.Rolacle
	nodeID     types.NodeID
	signer     *signing.EdSigner
	publisher  pubsub.Publisher
	layerClock layerClock
	beacon     system.BeaconGetter

	mu          sync.Mutex
	certifyMsgs map[types.LayerID]map[types.BlockID]*certInfo
}

// NewCertifier creates new block certifier.
func NewCertifier(
	db *sql.Database, o hare.Rolacle, n types.NodeID, s *signing.EdSigner, p pubsub.Publisher, lc layerClock, b system.BeaconGetter,
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
		publisher:   p,
		layerClock:  lc,
		beacon:      b,
		certifyMsgs: make(map[types.LayerID]map[types.BlockID]*certInfo),
	}
	for _, opt := range opts {
		opt(c)
	}
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
	for layer := c.layerClock.GetCurrentLayer(); ; layer = layer.Add(1) {
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
	current := c.layerClock.GetCurrentLayer()
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
	logger.Info("certifier registered")

	c.mu.Lock()
	defer c.mu.Unlock()
	c.createIfNeeded(lid, bid)
	c.certifyMsgs[lid][bid].registered = true
	return c.tryGenCert(logger, lid, bid)
}

// CertifyIfEligible signs the hare output, along with its role proof as a certifier, and gossip the CertifyMessage
// if the node is eligible to be a certifier.
func (c *Certifier) CertifyIfEligible(ctx context.Context, logger log.Log, lid types.LayerID, bid types.BlockID) error {
	if _, err := c.beacon.GetBeacon(lid.GetEpoch()); err != nil {
		return errBeaconNotAvailable
	}
	// check if the node is eligible to certify the hare output
	proof, err := c.oracle.Proof(ctx, lid, eligibility.CertifyRound)
	if err != nil {
		logger.With().Error("failed to get eligibility proof to certify", log.Err(err))
		return err
	}

	eligibilityCount, err := c.oracle.CalcEligibility(ctx, lid, eligibility.CertifyRound, c.cfg.CommitteeSize, c.nodeID, proof)
	if err != nil {
		logger.With().Error("failed to check eligibility to certify", log.Err(err))
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
	}
	msg.Signature = c.signer.Sign(msg.Bytes())
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

// HandleCertifyMessage is the gossip receiver for certify message.
func (c *Certifier) HandleCertifyMessage(ctx context.Context, peer p2p.Peer, msg []byte) pubsub.ValidationResult {
	if c.isShuttingDown() {
		return pubsub.ValidationIgnore
	}

	err := c.handleRawCertifyMsg(ctx, msg)
	switch {
	case err == nil:
		return pubsub.ValidationAccept
	case errors.Is(err, errMalformedData):
		c.logger.WithContext(ctx).With().Warning("malformed cert msg", log.Stringer("peer", peer), log.Err(err))
		return pubsub.ValidationReject
	default:
		return pubsub.ValidationIgnore
	}
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
	eligibilityCnt := uint16(0)
	for _, msg := range cert.Signatures {
		if err := validate(ctx, logger, c.oracle, c.cfg.CommitteeSize, msg); err != nil {
			continue
		}
		eligibilityCnt += msg.EligibilityCnt
	}
	if int(eligibilityCnt) < c.cfg.CertifyThreshold {
		logger.With().Warning("certificate not meeting threshold",
			log.Int("num_msgs", len(cert.Signatures)),
			log.Int("threshold", c.cfg.CertifyThreshold),
			log.Uint16("eligibility_count", eligibilityCnt))
		return errInvalidCert
	}
	if err := c.save(logger, lid, cert); err != nil {
		return err
	}
	return nil
}

func (c *Certifier) certified(lid types.LayerID, bid types.BlockID) bool {
	if _, ok := c.certifyMsgs[lid]; ok {
		if _, ok := c.certifyMsgs[lid][bid]; ok {
			return c.certifyMsgs[lid][bid].done
		}
	}
	return false
}

func expectedLayer(clock layerClock, lid types.LayerID) bool {
	current := clock.GetCurrentLayer()
	// only accept early msgs within a range and with limited size to prevent DOS
	return !lid.Before(current) && !lid.After(current.Add(numEarlyLayers))
}

func (c *Certifier) handleRawCertifyMsg(ctx context.Context, data []byte) error {
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

	if !expectedLayer(c.layerClock, lid) {
		logger.Debug("received message for unexpected layer")
		return errUnexpectedMsg
	}

	if c.certified(lid, bid) {
		// should still gossip this msg to peers even when this node has created a certificate
		return nil
	}

	if err := validate(ctx, logger, c.oracle, c.cfg.CommitteeSize, msg); err != nil {
		return err
	}

	if err := c.saveMessage(logger, msg); err != nil {
		return errInternal
	}
	return nil
}

func validate(ctx context.Context, logger log.Log, oracle hare.Rolacle, committeeSize int, msg types.CertifyMessage) error {
	// extract public key from signature
	pubkey, err := ed25519.ExtractPublicKey(msg.Bytes(), msg.Signature)
	if err != nil {
		return fmt.Errorf("%w: cert msg extract key: %v", errMalformedData, err.Error())
	}
	nid := types.BytesToNodeID(pubkey)
	valid, err := oracle.Validate(ctx, msg.LayerID, eligibility.CertifyRound, committeeSize, nid, msg.Proof, msg.EligibilityCnt)
	if err != nil {
		logger.With().Warning("failed to validate cert msg", log.Err(err))
		return errInternal
	}
	if !valid {
		logger.With().Warning("oracle deemed cert msg invalid", log.Stringer("smesher", nid))
		return errInvalidCertMsg
	}
	return nil
}

func (c *Certifier) saveMessage(logger log.Log, msg types.CertifyMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	lid := msg.LayerID
	bid := msg.BlockID
	c.createIfNeeded(lid, bid)

	c.certifyMsgs[lid][bid].signatures = append(c.certifyMsgs[lid][bid].signatures, msg)
	c.certifyMsgs[lid][bid].totalEligibility += msg.EligibilityCnt
	logger.With().Debug("saved certify msg",
		log.Uint16("eligibility_count", c.certifyMsgs[lid][bid].totalEligibility),
		log.Int("num_msg", len(c.certifyMsgs[lid][bid].signatures)))

	if c.certifyMsgs[lid][bid].registered {
		return c.tryGenCert(logger, lid, bid)
	}
	return nil
}

func (c *Certifier) tryGenCert(logger log.Log, lid types.LayerID, bid types.BlockID) error {
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
		log.Int("num_msg", len(c.certifyMsgs[lid][bid].signatures)))
	cert := &types.Certificate{
		BlockID:    bid,
		Signatures: c.certifyMsgs[lid][bid].signatures,
	}
	if err := c.save(logger, lid, cert); err != nil {
		return err
	}
	c.certifyMsgs[lid][bid].done = true
	return nil
}

func (c *Certifier) save(logger log.Log, lid types.LayerID, cert *types.Certificate) error {
	if err := layers.SetHareOutputWithCert(c.db, lid, cert); err != nil {
		logger.Error("failed to save block cert", log.Err(err))
		return err
	}
	return nil
}
