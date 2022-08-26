package blocks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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

var (
	errInvalidCert        = errors.New("invalid certificate")
	errInvalidCertMsg     = errors.New("invalid cert msg")
	errUnexpectedMsg      = errors.New("unexpected lid/bid")
	errInternal           = errors.New("internal")
	errBeaconNotAvailable = errors.New("beacon not available")
)

// CertConfig is the config for Certifier.
type CertConfig struct {
	CommitteeSize         int
	CertifyThreshold      int
	SignatureWaitDuration time.Duration
	NumLayersToKeep       uint32
}

func defaultCertConfig() CertConfig {
	return CertConfig{
		CommitteeSize:         10,
		CertifyThreshold:      6,
		SignatureWaitDuration: time.Second * 10,
		NumLayersToKeep:       5,
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
	bid              types.BlockID
	deadline         time.Time
	done             bool
	totalEligibility uint16
	signatures       []types.CertifyMessage
}

type certState int

const (
	unknown certState = iota
	early
	pending
	certified
)

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
	certifyMsgs map[types.LayerID]*certInfo
	buffer      map[types.LayerID][]*types.CertifyMessage
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
		certifyMsgs: make(map[types.LayerID]*certInfo),
		buffer:      make(map[types.LayerID][]*types.CertifyMessage),
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
			_ = c.genCertificate()
		case <-c.ctx.Done():
			return fmt.Errorf("context done: %w", c.ctx.Err())
		}
	}
}

// RegisterDeadline register the deadline to stop waiting for enough signatures to generate
// the certificate.
func (c *Certifier) RegisterDeadline(ctx context.Context, lid types.LayerID, bid types.BlockID, now time.Time) error {
	logger := c.logger.WithContext(ctx).WithFields(lid, bid)
	logger.Info("certifier for layer registered")
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.certifyMsgs[lid]; !ok {
		c.certifyMsgs[lid] = &certInfo{
			bid:        bid,
			deadline:   now.Add(c.cfg.SignatureWaitDuration),
			signatures: make([]types.CertifyMessage, 0, c.cfg.CommitteeSize),
		}
	}
	for _, msg := range c.buffer[lid] {
		if err := c.saveMessageLocked(logger, *msg); err != nil {
			return err
		}
	}
	delete(c.buffer, lid)
	return nil
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

func (c *Certifier) getCertState(logger log.Log, lid types.LayerID, bid types.BlockID) (certState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getCertStateLocked(logger, lid, bid)
}

func (c *Certifier) getCertStateLocked(logger log.Log, lid types.LayerID, bid types.BlockID) (certState, error) {
	if ci, ok := c.certifyMsgs[lid]; !ok {
		current := c.layerClock.GetCurrentLayer()
		// only accept early msgs within a range and with limited size to prevent DOS
		if !lid.Before(current) && lid.Before(current.Add(c.cfg.NumLayersToKeep)) {
			if c.buffer[lid] == nil || len(c.buffer[lid]) < c.cfg.CommitteeSize {
				logger.Debug("received early certify message")
				return early, nil
			}
		}
		logger.Warning("layer not registered for cert")
		return unknown, errUnexpectedMsg
	} else if bid != ci.bid {
		logger.With().Warning("block not registered for cert", log.Stringer("cert_block_id", ci.bid))
		return unknown, errUnexpectedMsg
	}

	if c.certifyMsgs[lid].done {
		return certified, nil
	}
	return pending, nil
}

// HandleSyncedCertificate handles Certificate from sync.
func (c *Certifier) HandleSyncedCertificate(ctx context.Context, lid types.LayerID, cert *types.Certificate) error {
	logger := c.logger.WithContext(ctx).WithFields(lid, cert.BlockID)
	logger.Info("processing synced certificate")
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

	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.buffer, lid)
	return nil
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
	if state, err := c.getCertState(logger, lid, bid); err != nil {
		return err
	} else if state == certified {
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
	return c.saveMessageLocked(logger, msg)
}

func (c *Certifier) saveMessageLocked(logger log.Log, msg types.CertifyMessage) error {
	lid := msg.LayerID
	bid := msg.BlockID
	state, err := c.getCertStateLocked(logger, lid, bid)
	if err != nil {
		return err
	} else if state != early && state != pending { // already certified
		return nil
	}

	if state == early {
		if _, ok := c.buffer[lid]; !ok {
			c.buffer[lid] = make([]*types.CertifyMessage, 0, c.cfg.CommitteeSize)
		}
		if len(c.buffer[lid]) >= c.cfg.CommitteeSize {
			logger.Warning("too many early certify msgs")
			return errUnexpectedMsg
		}
		c.buffer[lid] = append(c.buffer[lid], &msg)
		logger.With().Debug("early certify msg cached")
		return nil
	}

	if _, ok := c.certifyMsgs[lid]; !ok {
		logger.Fatal("missing layer in cache")
	}

	c.certifyMsgs[lid].signatures = append(c.certifyMsgs[lid].signatures, msg)
	c.certifyMsgs[lid].totalEligibility += msg.EligibilityCnt
	logger.With().Debug("saved certify msg",
		log.Uint16("eligibility_count", c.certifyMsgs[lid].totalEligibility),
		log.Int("num_msg", len(c.certifyMsgs[lid].signatures)))
	return nil
}

func (c *Certifier) genCertificate() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := types.GetEffectiveGenesis()
	current := c.layerClock.GetCurrentLayer()
	if current.Uint32() > c.cfg.NumLayersToKeep {
		cutoff = c.layerClock.GetCurrentLayer().Sub(c.cfg.NumLayersToKeep)
	}
	for lid, ci := range c.certifyMsgs {
		c.tryGenCert(c.logger.WithFields(lid, ci.bid), lid, ci.bid)
		if lid.Before(cutoff) {
			c.logger.With().Info("removing layer from cert registration", lid)
			delete(c.certifyMsgs, lid)
			delete(c.buffer, lid)
		}
	}
	return nil
}

func (c *Certifier) tryGenCert(logger log.Log, lid types.LayerID, bid types.BlockID) {
	if c.certifyMsgs[lid].done ||
		c.certifyMsgs[lid].totalEligibility < uint16(c.cfg.CertifyThreshold) {
		return
	}

	// even though there are enough eligibility count, we collect as many certify messages as we can.
	// the reason being that we will serve the certificate to the peers, and their local  oracle have
	// different active set/total weights from ours.
	if time.Now().Before(c.certifyMsgs[lid].deadline) {
		logger.With().Info("enough certify msgs accumulated. trying to wait for more")
		return
	}

	logger.With().Info("generating certificate",
		log.Uint16("eligibility_count", c.certifyMsgs[lid].totalEligibility),
		log.Int("num_msg", len(c.certifyMsgs[lid].signatures)))
	cert := &types.Certificate{
		BlockID:    bid,
		Signatures: c.certifyMsgs[lid].signatures,
	}
	if err := c.save(logger, lid, cert); err != nil {
		return
	}

	c.certifyMsgs[lid].done = true
}

func (c *Certifier) save(logger log.Log, lid types.LayerID, cert *types.Certificate) error {
	if err := layers.SetHareOutputWithCert(c.db, lid, cert); err != nil {
		logger.Error("failed to save block cert", log.Err(err))
		return err
	}
	return nil
}
