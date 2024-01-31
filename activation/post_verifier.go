package activation

import (
	"context"
	"fmt"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
)

type verifyPostJob struct {
	ctx      context.Context // context of Verify() call
	proof    *shared.Proof
	metadata *shared.ProofMetadata
	opts     []verifying.OptionFunc
	result   chan error
}

type autoscaler struct {
	sub *events.BufferedSubscription[events.UserEvent]
}

func newAutoscaler() (*autoscaler, error) {
	sub, err := events.SubscribeMatched(func(t *events.UserEvent) bool {
		switch t.Event.Details.(type) {
		case *pb.Event_PostStart:
			return true
		case *pb.Event_PostComplete:
			return true
		default:
			return false
		}
	}, events.WithBuffer(5))

	return &autoscaler{sub: sub}, err
}

func (a autoscaler) run(stop chan struct{}, s scaler, min, target int) {
	for {
		select {
		case e := <-a.sub.Out():
			switch e.Event.Details.(type) {
			case *pb.Event_PostStart:
				s.scale(min)
			case *pb.Event_PostComplete:
				s.scale(target)
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
	opts ...verifying.OptionFunc,
) error {
	v.logger.Debug("verifying post", zap.Stringer("proof_node_id", types.BytesToNodeID(m.NodeId)))
	return v.ProofVerifier.Verify(p, m, v.cfg, v.logger, opts...)
}

type postVerifierOpts struct {
	opts           PostProofVerifyingOpts
	prioritizedIds []types.NodeID
	autoscaling    bool
}

type PostVerifierOpt func(v *postVerifierOpts)

func WithVerifyingOpts(opts PostProofVerifyingOpts) PostVerifierOpt {
	return func(v *postVerifierOpts) {
		v.opts = opts
	}
}

func PrioritizedIDs(ids ...types.NodeID) PostVerifierOpt {
	return func(v *postVerifierOpts) {
		v.prioritizedIds = ids
	}
}

func WithAutoscaling() PostVerifierOpt {
	return func(v *postVerifierOpts) {
		v.autoscaling = true
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
	if options.autoscaling && minWorkers != workers {
		offloadingVerifier.autoscale(minWorkers, workers)
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

	v.log.Info("starting post verifier")
	v.scale(numWorkers)
	v.log.Info("started post verifier")
	return v
}

// Turn on automatic scaling of the number of workers.
// The number of workers will be scaled between `min` and `target` (inclusive).
func (v *offloadingPostVerifier) autoscale(min, target int) {
	a, err := newAutoscaler()
	if err != nil {
		v.log.Panic("failed to create autoscaler", zap.Error(err))
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
		stopping := make([]<-chan struct{}, 0, len(toStop))
		for _, worker := range toStop {
			close(worker.stop)
			stopping = append(stopping, worker.stopped)
		}
		for _, stopped := range stopping {
			<-stopped
		}
	}
}

func (v *offloadingPostVerifier) Verify(
	ctx context.Context,
	p *shared.Proof,
	m *shared.ProofMetadata,
	opts ...verifying.OptionFunc,
) error {
	job := &verifyPostJob{
		ctx:      ctx,
		proof:    p,
		metadata: m,
		opts:     opts,
		result:   make(chan error, 1),
	}

	metrics.PostVerificationQueue.Inc()
	defer metrics.PostVerificationQueue.Dec()

	var jobChannel chan<- *verifyPostJob
	_, prioritize := v.prioritizedIds[types.BytesToNodeID(m.NodeId)]
	switch {
	case prioritize:
		v.log.Debug("prioritizing post verification", zap.Stringer("proof_node_id", types.BytesToNodeID(m.NodeId)))
		jobChannel = v.prioritized
	default:
		jobChannel = v.jobs
	}

	select {
	case jobChannel <- job:
	case <-v.stop:
		return fmt.Errorf("verifier is closed")
	case <-ctx.Done():
		return fmt.Errorf("submitting verifying job: %w", ctx.Err())
	}

	select {
	case res := <-job.result:
		return res
	case <-v.stop:
		return fmt.Errorf("verifier is closed")
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
	v.log.Info("stopping post verifier")
	close(v.stop)
	v.eg.Wait()

	v.verifier.Close()
	v.log.Info("stopped post verifier")
	return nil
}

func (w *postVerifierWorker) start() {
	w.log.Info("starting")
	defer w.log.Info("stopped")
	defer close(w.stopped)

	for {
		// First try to process a prioritized job.
		select {
		case job := <-w.prioritized:
			job.result <- w.verifier.Verify(job.ctx, job.proof, job.metadata, job.opts...)
		default:
			select {
			case <-w.shutdown:
				return
			case <-w.stop:
				return
			case job := <-w.prioritized:
				job.result <- w.verifier.Verify(job.ctx, job.proof, job.metadata, job.opts...)
			case job := <-w.jobs:
				job.result <- w.verifier.Verify(job.ctx, job.proof, job.metadata, job.opts...)
			}
		}
	}
}

type noopPostVerifier struct{}

func (v *noopPostVerifier) Verify(
	_ context.Context,
	_ *shared.Proof,
	_ *shared.ProofMetadata,
	_ ...verifying.OptionFunc,
) error {
	return nil
}

func (v *noopPostVerifier) Close() error {
	return nil
}
