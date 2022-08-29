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
)

var (
	errKnownCertMsg   = errors.New("known certify msg")
	errMsgTooOld      = errors.New("cert msg too old")
	errInvalidCertMsg = errors.New("invalid cert msg")
	errUnexpectedMsg  = errors.New("unexpected lid/bid")
	errInternal       = errors.New("internal")
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
	pending
	certified
	expired
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

	mu          sync.Mutex
	certifyMsgs map[types.LayerID]*certInfo
}

// NewCertifier creates new block certifier.
func NewCertifier(
	db *sql.Database, o hare.Rolacle, n types.NodeID, s *signing.EdSigner, p pubsub.Publisher, lc layerClock,
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
		certifyMsgs: make(map[types.LayerID]*certInfo),
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
			_ = c.prune()
		case <-c.ctx.Done():
			return fmt.Errorf("context done: %w", c.ctx.Err())
		}
	}
}

// RegisterDeadline register the deadline to stop waiting for enough signatures to generate
// the certificate.
func (c *Certifier) RegisterDeadline(lid types.LayerID, bid types.BlockID, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.certifyMsgs[lid]; !ok {
		c.certifyMsgs[lid] = &certInfo{
			bid:        bid,
			deadline:   now.Add(c.cfg.SignatureWaitDuration),
			signatures: make([]types.CertifyMessage, 0, c.cfg.CommitteeSize),
		}
	}
}

// CertifyIfEligible signs the hare output, along with its role proof as a certifier, and gossip the CertifyMessage
// if the node is eligible to be a certifier.
func (c *Certifier) CertifyIfEligible(ctx context.Context, logger log.Log, lid types.LayerID, bid types.BlockID) error {
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
func (c *Certifier) HandleCertifyMessage(ctx context.Context, _ p2p.Peer, msg []byte) pubsub.ValidationResult {
	if c.isShuttingDown() {
		return pubsub.ValidationIgnore
	}

	err := c.handleRawCertifyMsg(ctx, msg)
	switch {
	case err == nil:
		return pubsub.ValidationAccept
	case errors.Is(err, errMalformedData):
		return pubsub.ValidationReject
	case errors.Is(err, errKnownCertMsg) || errors.Is(err, errMsgTooOld):
		return pubsub.ValidationIgnore
	default:
		c.logger.WithContext(ctx).With().Error("failed to process certify message gossip", log.Err(err))
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
		logger.Warning("layer not registered for cert")
		return unknown, errUnexpectedMsg
	} else if bid != ci.bid {
		logger.Warning("block not registered for cert")
		return unknown, errUnexpectedMsg
	}

	if time.Now().After(c.certifyMsgs[lid].deadline) {
		logger.With().Debug("cert msg received after deadline", log.Time("deadline", c.certifyMsgs[lid].deadline))
		return expired, nil
	}
	if c.certifyMsgs[lid].done {
		return certified, nil
	}
	return pending, nil
}

// HandleSyncedCertificate handles Certificate from sync.
func (c *Certifier) HandleSyncedCertificate(ctx context.Context, lid types.LayerID, cert *types.Certificate) error {
	logger := c.logger.WithContext(ctx).WithFields(cert.BlockID)
	for _, msg := range cert.Signatures {
		if err := c.validate(ctx, logger, msg); err != nil {
			return err
		}
	}
	if err := c.save(logger, lid, cert); err != nil {
		return err
	}
	return nil
}

func (c *Certifier) handleRawCertifyMsg(ctx context.Context, data []byte) error {
	logger := c.logger.WithContext(ctx)
	var msg types.CertifyMessage
	if err := codec.Decode(data, &msg); err != nil {
		logger.With().Error("malformed cert msg", log.Err(err))
		return errMalformedData
	}
	logger = logger.WithFields(msg.LayerID, msg.BlockID)
	if err := c.validate(ctx, logger, msg); err != nil {
		return err
	}
	if err := c.saveMessage(logger, msg); err != nil {
		return errInternal
	}
	return nil
}

func (c *Certifier) validate(ctx context.Context, logger log.Log, msg types.CertifyMessage) error {
	lid := msg.LayerID
	bid := msg.BlockID
	if state, err := c.getCertState(logger, lid, bid); err != nil {
		return err
	} else if state == expired {
		return errMsgTooOld
	} else if state == certified {
		// should still gossip this msg to peers even when this node has created a certificate
		return nil
	}

	// extract public key from signature
	pubkey, err := ed25519.ExtractPublicKey(msg.Bytes(), msg.Signature)
	if err != nil {
		return fmt.Errorf("%w: cert msg extract key: %v", errMalformedData, err.Error())
	}
	nid := types.BytesToNodeID(pubkey)
	valid, err := c.oracle.Validate(ctx, msg.LayerID, eligibility.CertifyRound, c.cfg.CommitteeSize, nid, msg.Proof, msg.EligibilityCnt)
	if err != nil {
		logger.With().Warning("failed to validate cert msg", log.Err(err))
		return errInternal
	}
	if !valid {
		logger.With().Warning("invalid cert msg", log.Err(err))
		return errInvalidCertMsg
	}
	return nil
}

func (c *Certifier) saveMessage(logger log.Log, msg types.CertifyMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	lid := msg.LayerID
	bid := msg.BlockID
	if state, err := c.getCertStateLocked(logger, lid, bid); err != nil {
		return err
	} else if state != pending { // already certified
		return nil
	}

	if _, ok := c.certifyMsgs[lid]; !ok {
		logger.Fatal("missing layer in cache")
	}
	c.certifyMsgs[lid].signatures = append(c.certifyMsgs[lid].signatures, msg)
	c.certifyMsgs[lid].totalEligibility += msg.EligibilityCnt

	if c.certifyMsgs[lid].totalEligibility < uint16(c.cfg.CertifyThreshold) {
		return nil
	}

	cert := &types.Certificate{
		BlockID:    bid,
		Signatures: c.certifyMsgs[lid].signatures,
	}
	if err := c.save(logger, lid, cert); err != nil {
		return err
	}

	c.certifyMsgs[lid].done = true
	return nil
}

func (c *Certifier) prune() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := c.layerClock.GetCurrentLayer().Sub(c.cfg.NumLayersToKeep)
	for lid := range c.certifyMsgs {
		if lid.Before(cutoff) {
			delete(c.certifyMsgs, lid)
		}
	}

	return nil
}

func (c *Certifier) save(logger log.Log, lid types.LayerID, cert *types.Certificate) error {
	if err := layers.SetHareOutputWithCert(c.db, lid, cert); err != nil {
		logger.Error("failed to save block cert", log.Err(err))
		return err
	}
	return nil
}
