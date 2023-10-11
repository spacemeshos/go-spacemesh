package activation

import (
	"context"
	"fmt"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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
	log     log.Log
	workers []*postVerifierWorker
	channel chan<- *verifyPostJob
}

type postVerifierWorker struct {
	verifier PostVerifier
	log      log.Log
	channel  <-chan *verifyPostJob
}

type postVerifier struct {
	*verifying.ProofVerifier
	logger *zap.Logger
	cfg    config.Config
}

func (v *postVerifier) Verify(ctx context.Context, p *shared.Proof, m *shared.ProofMetadata, opts ...verifying.OptionFunc) error {
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
func NewOffloadingPostVerifier(verifiers []PostVerifier, logger log.Log) *OffloadingPostVerifier {
	numWorkers := len(verifiers)
	channel := make(chan *verifyPostJob, numWorkers)
	workers := make([]*postVerifierWorker, 0, numWorkers)

	for i, verifier := range verifiers {
		workers = append(workers, &postVerifierWorker{
			verifier: verifier,
			log:      logger.Named(fmt.Sprintf("worker-%d", i)),
			channel:  channel,
		})
	}
	logger.With().Info("created post verifier", log.Int("num_workers", numWorkers))

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	v := &OffloadingPostVerifier{
		log:     logger,
		workers: workers,
		channel: channel,
		stopped: stopped,
		stop: func() {
			cancel()
			select {
			case <-stopped:
			default:
				close(stopped)
			}
		},
	}

	v.log.Info("starting post verifier")
	for _, worker := range v.workers {
		worker := worker
		v.eg.Go(func() error { return worker.start(ctx) })
	}
	v.log.Info("started post verifier")
	return v
}

func (v *OffloadingPostVerifier) Verify(ctx context.Context, p *shared.Proof, m *shared.ProofMetadata, opts ...verifying.OptionFunc) error {
	job := &verifyPostJob{
		proof:    p,
		metadata: m,
		opts:     opts,
		result:   make(chan error, 1),
	}

	select {
	case v.channel <- job:
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

	for _, worker := range v.workers {
		if err := worker.verifier.Close(); err != nil {
			return err
		}
	}
	v.log.Info("stopped post verifier")
	return nil
}

func (w *postVerifierWorker) start(ctx context.Context) error {
	w.log.Info("starting post proof verifier worker")
	for {
		select {
		case <-ctx.Done():
			w.log.Info("stopped post proof verifier worker")
			return ctx.Err()
		case job := <-w.channel:
			job.result <- w.verifier.Verify(ctx, job.proof, job.metadata, job.opts...)
		}
	}
}
