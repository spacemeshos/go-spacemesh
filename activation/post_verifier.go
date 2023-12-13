package activation

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type verifyPostJob struct {
	proof    *shared.Proof
	metadata *shared.ProofMetadata
	opts     []verifying.OptionFunc
	result   chan error
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
	cancel   atomic.Pointer[context.CancelFunc]
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
	v.Scale(numWorkers)
	v.log.Info("started post verifier")
	return v
}

// Scale scales the number of workers to the given number.
func (v *OffloadingPostVerifier) Scale(numWorkers int) {
	v.log.Info("scaling post verifier", zap.Int("current", len(v.workers)), zap.Int("new", numWorkers))

	if numWorkers > len(v.workers) {
		// scale up
		for i := 0; i < numWorkers-len(v.workers); i++ {
			w := newWorker(v.verifier, v.log.Named(fmt.Sprintf("worker-%d", len(v.workers))), v.jobs)
			v.workers = append(v.workers, w)
			v.eg.Go(func() error { return w.start(v.workersCtx) })
		}
	} else if numWorkers < len(v.workers) {
		// scale down
		toKeep, toStop := v.workers[:numWorkers], v.workers[numWorkers:]
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

func newWorker(verifier PostVerifier, logger *zap.Logger, channel <-chan *verifyPostJob) *postVerifierWorker {
	return &postVerifierWorker{
		verifier: verifier,
		log:      logger,
		jobs:     channel,
	}
}

func (w *postVerifierWorker) start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	if !w.cancel.CompareAndSwap(nil, &cancel) {
		return fmt.Errorf("worker already started")
	}
	w.log.Info("starting post proof verifier worker")
	defer w.log.Info("stopped post proof verifier worker")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case job := <-w.jobs:
			job.result <- w.verifier.Verify(ctx, job.proof, job.metadata, job.opts...)
		}
	}
}

func (w *postVerifierWorker) stop() {
	if cancel := w.cancel.Swap(nil); cancel != nil {
		w.log.Info("stopping post proof verifier worker")
		(*cancel)()
	} else {
		w.log.Warn("tried to stop worker that was not started")
	}
}
