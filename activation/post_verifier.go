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

func (a autoscaler) run(stop chan struct{}, s scaler, min, target int) {
	for {
		select {
		case e := <-a.sub.Out():
			switch e.Event.Details.(type) {
			case (*pb.Event_PostStart):
				s.scale(min)
			case (*pb.Event_PostComplete):
				s.scale(target)
			}
		case <-stop:
			a.sub.Close()
			return
		}
	}
}

type OffloadingPostVerifier struct {
	eg       errgroup.Group
	log      *zap.Logger
	verifier PostVerifier
	workers  []*postVerifierWorker
	jobs     chan *verifyPostJob
	stop     chan struct{} // signal to stop all goroutines
}

type postVerifierWorker struct {
	verifier PostVerifier
	log      *zap.Logger
	jobs     <-chan *verifyPostJob
	stop     chan struct{} // signal to stop this worker
	shutdown chan struct{} // signal that the verifier is closing
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
	v := &OffloadingPostVerifier{
		log:      logger,
		workers:  make([]*postVerifierWorker, 0, numWorkers),
		jobs:     make(chan *verifyPostJob, numWorkers),
		stop:     make(chan struct{}),
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
	v.eg.Go(func() error { a.run(v.stop, v, min, target); return nil })
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
			w := &postVerifierWorker{
				verifier: v.verifier,
				log:      v.log.Named(fmt.Sprintf("worker-%d", len(v.workers))),
				jobs:     v.jobs,
				stop:     make(chan struct{}),
				shutdown: v.stop,
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
	}
}

func (v *OffloadingPostVerifier) Verify(
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

	select {
	case v.jobs <- job:
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

func (v *OffloadingPostVerifier) Close() error {
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

	for {
		select {
		case <-w.shutdown:
			return
		case <-w.stop:
			return
		case job := <-w.jobs:
			job.result <- w.verifier.Verify(job.ctx, job.proof, job.metadata, job.opts...)
		}
	}
}
