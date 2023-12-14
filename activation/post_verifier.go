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

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
)

type verifyPostJob struct {
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
		case (*pb.Event_PostStart):
			return true
		case (*pb.Event_PostComplete):
			return true
		default:
			return false
		}
	}, events.WithBuffer(5))

	return &autoscaler{sub: sub}, err
}

func (a autoscaler) run(ctx context.Context, s scaler, min, target int) error {
	for {
		select {
		case e := <-a.sub.Out():
			switch e.Event.Details.(type) {
			case (*pb.Event_PostStart):
				s.scale(min)
			case (*pb.Event_PostComplete):
				s.scale(target)
			}
		case <-ctx.Done():
			a.sub.Close()
			return ctx.Err()
		}
	}
}

type OffloadingPostVerifier struct {
	eg      errgroup.Group
	stop    context.CancelFunc
	stopped <-chan struct{}
	log     *zap.Logger

	workersCtx context.Context
	workers    []*postVerifierWorker
	jobs       chan *verifyPostJob
	verifier   PostVerifier
}

type postVerifierWorker struct {
	verifier PostVerifier
	log      *zap.Logger
	jobs     <-chan *verifyPostJob
	stop     context.CancelFunc
}

type postVerifier struct {
	*verifying.ProofVerifier
	logger *zap.Logger
	cfg    config.Config
}

func (v *postVerifier) Verify(
	ctx context.Context,
	p *shared.Proof,
	m *shared.ProofMetadata,
	opts ...verifying.OptionFunc,
) error {
	v.logger.Debug("verifying post", zap.Stringer("proof_node_id", types.BytesToNodeID(m.NodeId)))
	return v.ProofVerifier.Verify(p, m, v.cfg, v.logger, opts...)
}

// NewPostVerifier creates a new post verifier.
func NewPostVerifier(cfg PostConfig, logger *zap.Logger, opts ...verifying.OptionFunc) (PostVerifier, error) {
	verifier, err := verifying.NewProofVerifier(opts...)
	if err != nil {
		return nil, err
	}

	return &postVerifier{logger: logger, ProofVerifier: verifier, cfg: cfg.ToConfig()}, nil
}

// NewOffloadingPostVerifier creates a new post proof verifier with the given number of workers.
// The verifier will distribute incoming proofs between the workers.
// It will block if all workers are busy.
//
// SAFETY: The `verifier` must be safe to use concurrently.
//
// The verifier must be closed after use with Close().
func NewOffloadingPostVerifier(verifier PostVerifier, numWorkers int, logger *zap.Logger) *OffloadingPostVerifier {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	v := &OffloadingPostVerifier{
		log:        logger,
		workersCtx: ctx,
		workers:    make([]*postVerifierWorker, 0, numWorkers),
		jobs:       make(chan *verifyPostJob, numWorkers),
		stopped:    stopped,
		stop: func() {
			cancel()
			select {
			case <-stopped:
			default:
				close(stopped)
			}
		},
		verifier: verifier,
	}

	v.log.Info("starting post verifier")
	v.scale(numWorkers)
	v.log.Info("started post verifier")
	return v
}

// Turn on automatic scaling of the number of workers.
// The number of workers will be scaled between `min` and `target` (inclusive).
func (v *OffloadingPostVerifier) Autoscale(min, target int) {
	a, err := newAutoscaler()
	if err != nil {
		v.log.Panic("failed to create autoscaler", zap.Error(err))
	}
	v.eg.Go(func() error { return a.run(v.workersCtx, v, min, target) })
}

// Scale the number of workers to the given number.
//
// SAFETY: Must not be called concurrently.
// This is satisified by the fact that the only caller is the autoscaler,
// which executes scale() serially.
func (v *OffloadingPostVerifier) scale(target int) {
	v.log.Info("scaling post verifier", zap.Int("current", len(v.workers)), zap.Int("new", target))

	if target > len(v.workers) {
		// scale up
		for i := len(v.workers); i < target; i++ {
			v.log.Debug("starting post verifier worker", zap.Int("worker", i))
			ctx, cancel := context.WithCancel(v.workersCtx)
			w := &postVerifierWorker{
				verifier: v.verifier,
				log:      v.log.Named(fmt.Sprintf("worker-%d", len(v.workers))),
				jobs:     v.jobs,
				stop:     cancel,
			}
			v.workers = append(v.workers, w)
			v.eg.Go(func() error { return w.start(ctx) })
		}
	} else if target < len(v.workers) {
		// scale down
		toKeep, toStop := v.workers[:target], v.workers[target:]
		v.workers = toKeep
		for _, worker := range toStop {
			worker.stop()
		}
	}
}

func (v *OffloadingPostVerifier) Verify(
	ctx context.Context,
	p *shared.Proof,
	m *shared.ProofMetadata,
	opts ...verifying.OptionFunc,
) error {
	job := &verifyPostJob{
		proof:    p,
		metadata: m,
		opts:     opts,
		result:   make(chan error, 1),
	}

	select {
	case v.jobs <- job:
	case <-v.stopped:
		return fmt.Errorf("verifier is closed")
	case <-ctx.Done():
		return fmt.Errorf("submitting verifying job: %w", ctx.Err())
	}

	select {
	case res := <-job.result:
		return res
	case <-v.stopped:
		return fmt.Errorf("verifier is closed")
	case <-ctx.Done():
		return fmt.Errorf("waiting for verification result: %w", ctx.Err())
	}
}

func (v *OffloadingPostVerifier) Close() error {
	v.log.Info("stopping post verifier")
	v.stop()
	v.eg.Wait()

	v.verifier.Close()
	v.log.Info("stopped post verifier")
	return nil
}

func (w *postVerifierWorker) start(ctx context.Context) error {
	w.log.Info("starting")
	defer w.log.Info("stopped")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case job := <-w.jobs:
			job.result <- w.verifier.Verify(ctx, job.proof, job.metadata, job.opts...)
		}
	}
}
