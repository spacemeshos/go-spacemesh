package activation

import (
	"context"
	"errors"
	"fmt"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
)

//go:generate mockgen -typed -source=post_verifier.go -destination=mocks/subscription.go -package=mocks

type verifyPostJob struct {
	ctx      context.Context // context of Verify() call
	proof    *shared.Proof
	metadata *shared.ProofMetadata
	opts     []verifying.OptionFunc
	result   chan error
}

type postStatesGetter interface {
	Get() map[types.NodeID]types.PostState
}

type subscription[T any] interface {
	Close()
	Out() <-chan T
	Full() <-chan struct{}
}
type autoscaler struct {
	sub        subscription[events.UserEvent]
	bufferSize int
	logger     *zap.Logger
	postStates postStatesGetter
}

func newAutoscaler(logger *zap.Logger, postStates postStatesGetter, bufferSize int) *autoscaler {
	return &autoscaler{
		bufferSize: bufferSize,
		postStates: postStates,
		logger:     logger.Named("autoscaler"),
	}
}

func (a *autoscaler) subscribe() error {
	sub, err := events.SubscribeMatched(func(t *events.UserEvent) bool {
		switch t.Event.Details.(type) {
		case *pb.Event_PostStart:
			return true
		case *pb.Event_PostComplete:
			return true
		default:
			return false
		}
	}, events.WithBuffer(a.bufferSize))
	if err != nil {
		return err
	}
	a.sub = sub
	return nil
}

func (a autoscaler) run(stop chan struct{}, s scaler, min, target int) {
	nodeIDs := make(map[types.NodeID]struct{})

	for {
		select {
		case <-a.sub.Full():
			a.logger.Warn("post autoscaler events subscription overflow, restarting")
			if err := a.subscribe(); err != nil {
				a.logger.Error("failed to restart post autoscaler events subscription", zap.Error(err))
				return
			}
			clear(nodeIDs)
			for id, state := range a.postStates.Get() {
				if state == types.PostStateProving {
					nodeIDs[id] = struct{}{}
				}
			}
			if len(nodeIDs) == 0 {
				s.scale(target)
			} else {
				s.scale(min)
			}
		case e := <-a.sub.Out():
			switch event := e.Event.Details.(type) {
			case *pb.Event_PostStart:
				nodeIDs[types.BytesToNodeID(event.PostStart.Smesher)] = struct{}{}
				s.scale(min)
			case *pb.Event_PostComplete:
				delete(nodeIDs, types.BytesToNodeID(event.PostComplete.Smesher))
				if len(nodeIDs) == 0 {
					s.scale(target)
				} else {
					a.logger.Debug(
						"not scaling up, some nodes are still proving",
						zap.Array("smesherIDs", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
							for id := range nodeIDs {
								enc.AppendString(id.ShortString())
							}
							return nil
						})))
				}
			}
		case <-stop:
			a.sub.Close()
			return
		}
	}
}

type offloadingPostVerifier struct {
	eg          errgroup.Group
	log         *zap.Logger
	verifier    PostVerifier
	workers     []*postVerifierWorker
	prioritized chan *verifyPostJob
	jobs        chan *verifyPostJob
	stop        chan struct{} // signal to stop all goroutines

	prioritizedIds map[types.NodeID]struct{}
}

type postVerifierWorker struct {
	verifier    PostVerifier
	log         *zap.Logger
	prioritized <-chan *verifyPostJob
	jobs        <-chan *verifyPostJob
	stop        chan struct{} // signal to stop this worker
	stopped     chan struct{} // signal that this worker has stopped
	shutdown    chan struct{} // signal that the verifier is closing
}

type postVerifier struct {
	*verifying.ProofVerifier
	logger *zap.Logger
	cfg    config.Config
}

func (v *postVerifier) Verify(
	_ context.Context,
	p *shared.Proof,
	m *shared.ProofMetadata,
	opts ...postVerifierOptionFunc,
) error {
	opt := applyOptions(opts...)

	v.logger.Debug("verifying post", zap.Stringer("proof_node_id", types.BytesToNodeID(m.NodeId)))
	return v.ProofVerifier.Verify(p, m, v.cfg, v.logger, opt.verifierOptions...)
}

type postVerifierOpts struct {
	opts           PostProofVerifyingOpts
	prioritizedIds []types.NodeID
	autoscaling    *struct {
		postStates postStatesGetter
	}
}

type PostVerifierOpt func(v *postVerifierOpts)

func WithVerifyingOpts(opts PostProofVerifyingOpts) PostVerifierOpt {
	return func(v *postVerifierOpts) {
		v.opts = opts
	}
}

func WithPrioritizedID(id types.NodeID) PostVerifierOpt {
	return func(v *postVerifierOpts) {
		v.prioritizedIds = append(v.prioritizedIds, id)
	}
}

func WithAutoscaling(postStates postStatesGetter) PostVerifierOpt {
	return func(v *postVerifierOpts) {
		v.autoscaling = &struct{ postStates postStatesGetter }{}
	}
}

// NewPostVerifier creates a new post verifier.
func NewPostVerifier(cfg PostConfig, logger *zap.Logger, opts ...PostVerifierOpt) (PostVerifier, error) {
	options := &postVerifierOpts{
		opts: DefaultPostVerifyingOpts(),
	}
	for _, o := range opts {
		o(options)
	}
	if options.opts.Disabled {
		logger.Warn("verifying post proofs is disabled")
		return &noopPostVerifier{}, nil
	}

	logger.Debug("creating post verifier")
	verifier, err := verifying.NewProofVerifier(verifying.WithPowFlags(options.opts.Flags.Value()))
	logger.Debug("created post verifier", zap.Error(err))
	if err != nil {
		return nil, err
	}
	workers := options.opts.Workers
	minWorkers := min(options.opts.MinWorkers, workers)
	offloadingVerifier := newOffloadingPostVerifier(
		&postVerifier{logger: logger, ProofVerifier: verifier, cfg: cfg.ToConfig()},
		workers,
		logger,
		options.prioritizedIds...,
	)
	if options.autoscaling != nil && minWorkers != workers {
		offloadingVerifier.autoscale(minWorkers, workers, options.autoscaling.postStates)
	}
	return offloadingVerifier, nil
}

// newOffloadingPostVerifier creates a new post proof verifier with the given number of workers.
// The verifier will distribute incoming proofs between the workers.
// It will block if all workers are busy.
//
// SAFETY: The `verifier` must be safe to use concurrently.
//
// The verifier must be closed after use with Close().
func newOffloadingPostVerifier(
	verifier PostVerifier,
	numWorkers int,
	logger *zap.Logger,
	prioritizedIds ...types.NodeID,
) *offloadingPostVerifier {
	v := &offloadingPostVerifier{
		log:            logger,
		workers:        make([]*postVerifierWorker, 0, numWorkers),
		prioritized:    make(chan *verifyPostJob, numWorkers),
		jobs:           make(chan *verifyPostJob, numWorkers),
		stop:           make(chan struct{}),
		verifier:       verifier,
		prioritizedIds: make(map[types.NodeID]struct{}),
	}
	for _, id := range prioritizedIds {
		v.prioritizedIds[id] = struct{}{}
	}

	v.log.Debug("starting post verifier")
	v.scale(numWorkers)
	v.log.Debug("started post verifier")
	return v
}

// Turn on automatic scaling of the number of workers.
// The number of workers will be scaled between `min` and `target` (inclusive).
func (v *offloadingPostVerifier) autoscale(min, target int, postStates postStatesGetter) {
	a := newAutoscaler(v.log, postStates, len(v.prioritizedIds)*3)
	if err := a.subscribe(); err != nil {
		v.log.Panic("failed to subscribe to post events", zap.Error(err))
	}
	v.eg.Go(func() error { a.run(v.stop, v, min, target); return nil })
}

// Scale the number of workers to the given number.
//
// SAFETY: Must not be called concurrently.
// This is satisfied by the fact that the only caller is the autoscaler,
// which executes scale() serially.
func (v *offloadingPostVerifier) scale(target int) {
	v.log.Info("scaling post verifier", zap.Int("current", len(v.workers)), zap.Int("new", target))

	if target > len(v.workers) {
		// scale up
		for i := len(v.workers); i < target; i++ {
			w := &postVerifierWorker{
				verifier:    v.verifier,
				log:         v.log.Named(fmt.Sprintf("worker-%d", len(v.workers))),
				prioritized: v.prioritized,
				jobs:        v.jobs,
				stop:        make(chan struct{}),
				stopped:     make(chan struct{}),
				shutdown:    v.stop,
			}
			v.workers = append(v.workers, w)
			v.eg.Go(func() error { w.start(); return nil })
		}
	} else if target < len(v.workers) {
		// scale down
		toKeep, toStop := v.workers[:target], v.workers[target:]
		v.workers = toKeep
		for _, worker := range toStop {
			close(worker.stop)
		}
		for _, worker := range toStop {
			<-worker.stopped
		}
	}
}

// Verify creates a Job from given parameters, adds to jobs queue (prioritized or not)
// and waits for result of Job execution.
func (v *offloadingPostVerifier) Verify(
	ctx context.Context,
	p *shared.Proof,
	m *shared.ProofMetadata,
	opts ...postVerifierOptionFunc,
) error {
	opt := applyOptions(opts...)

	job := &verifyPostJob{
		ctx:      ctx,
		proof:    p,
		metadata: m,
		opts:     opt.verifierOptions,
		result:   make(chan error, 1),
	}

	metrics.PostVerificationQueue.Inc()
	defer metrics.PostVerificationQueue.Dec()

	jobChannel := v.jobs
	if opt.prioritized {
		v.log.Debug("prioritizing post verification call")
		jobChannel = v.prioritized
	} else {
		nodeID := types.BytesToNodeID(m.NodeId)
		if _, prioritized := v.prioritizedIds[nodeID]; prioritized {
			v.log.Debug("prioritizing post verification by Node ID", zap.Stringer("proof_node_id", nodeID))
			jobChannel = v.prioritized
		}
	}

	select {
	case jobChannel <- job:
	case <-v.stop:
		return errors.New("verifier is closed")
	case <-ctx.Done():
		return fmt.Errorf("submitting verifying job: %w", ctx.Err())
	}

	select {
	case res := <-job.result:
		return res
	case <-v.stop:
		return errors.New("verifier is closed")
	case <-ctx.Done():
		return fmt.Errorf("waiting for verification result: %w", ctx.Err())
	}
}

func (v *offloadingPostVerifier) Close() error {
	select {
	case <-v.stop:
		return nil
	default:
	}
	v.log.Debug("stopping post verifier")
	close(v.stop)
	v.eg.Wait()

	v.verifier.Close()
	v.log.Debug("stopped post verifier")
	return nil
}

func (w *postVerifierWorker) start() {
	w.log.Debug("starting")
	defer w.log.Debug("stopped")
	defer close(w.stopped)

	for {
		// First try to process a prioritized job.
		select {
		case job := <-w.prioritized:
			job.result <- w.verifier.Verify(job.ctx, job.proof, job.metadata, WithVerifierOptions(job.opts...))
		default:
			select {
			case <-w.shutdown:
				return
			case <-w.stop:
				return
			case job := <-w.prioritized:
				job.result <- w.verifier.Verify(job.ctx, job.proof, job.metadata, WithVerifierOptions(job.opts...))
			case job := <-w.jobs:
				job.result <- w.verifier.Verify(job.ctx, job.proof, job.metadata, WithVerifierOptions(job.opts...))
			}
		}
	}
}

type noopPostVerifier struct{}

func (v *noopPostVerifier) Verify(
	_ context.Context,
	_ *shared.Proof,
	_ *shared.ProofMetadata,
	_ ...postVerifierOptionFunc,
) error {
	return nil
}

func (v *noopPostVerifier) Close() error {
	return nil
}
