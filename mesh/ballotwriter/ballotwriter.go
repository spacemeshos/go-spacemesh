package ballotwriter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

var writerDelay = 100 * time.Millisecond

type BallotWriter struct {
	db     db
	logger *zap.Logger

	atxMu sync.Mutex
	timer *time.Ticker

	ballotBatch       map[types.BallotID]*types.Ballot // the current
	ballotBatchResult *batchResult
}

func New(db db, logger *zap.Logger) *BallotWriter {
	// create a stopped ticker that can be started later
	timer := time.NewTicker(writerDelay)
	timer.Stop()
	writer := &BallotWriter{
		db:     db,
		logger: logger,
		timer:  timer,
		ballotBatchResult: &batchResult{
			doneC: make(chan struct{}),
		},
		ballotBatch: make(map[types.BallotID]*types.Ballot),
	}
	return writer
}

// Start the forever-loop that flushes the ballots to the DB
// at-least every `writerDelay`. The caller is responsible
// to call Start in a different goroutine.
func (w *BallotWriter) Start(ctx context.Context) {
	w.timer.Reset(writerDelay)
	for {
		select {
		case <-ctx.Done():
			w.atxMu.Lock()
			w.ballotBatchResult.err = ctx.Err()
			close(w.ballotBatchResult.doneC)
			w.atxMu.Unlock()

			return
		case <-w.timer.C:
			// we hang on to this lock for the entire duration of this select case branch
			// as it simplifies the business logic.
			w.atxMu.Lock()
			startTime := time.Now()
			if len(w.ballotBatch) == 0 {
				w.atxMu.Unlock()
				continue
			}
			batch := w.ballotBatch
			res := w.ballotBatchResult // copy the result type
			BatchWriteCount.Inc()
			FlushBatchSize.Add(float64(len(batch)))

			var ballotAddDur, layerBallotDur time.Duration
			// we use a context.Background() because: on shutdown the canceling of the
			// context may exit the transaction halfway and leave the db in some state where it
			// causes crawshaw to panic on a "not all connections returned to pool".
			if err := w.db.WithTx(context.Background(), func(tx sql.Transaction) error {
				for _, ballot := range batch {
					if !ballot.IsMalicious() {
						layerBallotStart := time.Now()
						prev, err := ballots.LayerBallotByNodeID(tx, ballot.Layer, ballot.SmesherID)
						if err != nil && !errors.Is(err, sql.ErrNotFound) {
							return err
						}
						layerBallotDur += time.Since(layerBallotStart)

						if prev != nil && prev.ID() != ballot.ID() {
							var ballotProof wire.BallotProof
							for i, b := range []*types.Ballot{prev, ballot} {
								ballotProof.Messages[i] = wire.BallotProofMsg{
									InnerMsg: types.BallotMetadata{
										Layer:   b.Layer,
										MsgHash: types.BytesToHash(b.HashInnerBytes()),
									},
									Signature: b.Signature,
									SmesherID: b.SmesherID,
								}
							}
							proof := &wire.MalfeasanceProof{
								Layer: ballot.Layer,
								Proof: wire.Proof{
									Type: wire.MultipleBallots,
									Data: &ballotProof,
								},
							}
							encoded := codec.MustEncode(proof)
							if err := identities.SetMalicious(tx, ballot.SmesherID, encoded, time.Now()); err != nil {
								return fmt.Errorf("add malfeasance proof: %w", err)
							}
							ballot.SetMalicious()
							w.logger.Warn("smesher produced more than one ballot in the same layer",
								zap.Stringer("smesher", ballot.SmesherID),
								zap.Object("prev", prev),
								zap.Object("curr", ballot),
							)
						}
					}
					ballotAddStart := time.Now()
					if err := ballots.Add(tx, ballot); err != nil && !errors.Is(err, sql.ErrObjectExists) {
						return err
					}
					ballotAddDur += time.Since(ballotAddStart)
				}
				return nil
			}); err != nil {
				res.err = err
				ErroredBatchCount.Inc()
				w.logger.Error("flush ballots to db", zap.Error(err))
			}
			cleanupStart := time.Now()
			w.ballotBatchResult = &batchResult{doneC: make(chan struct{})}
			clear(w.ballotBatch)
			w.atxMu.Unlock()
			writeTime := time.Since(startTime)
			WriteTime.Add(float64(writeTime))
			WriteTimeHist.Observe(writeTime.Seconds())
			LayerBallotTime.Add(float64(layerBallotDur))
			BallotAddTime.Add(float64(ballotAddDur))
			CleanupTime.Add(float64(time.Since(cleanupStart)))
			close(res.doneC)
		}
	}
}

// Store a ballot. Will return the error encountered during the
// write to the db. May also return a context canceled error during
// shutdown.
func (w *BallotWriter) Store(b *types.Ballot) error {
	w.atxMu.Lock()
	br := w.ballotBatchResult
	w.ballotBatch[b.ID()] = b
	w.atxMu.Unlock()

	<-br.doneC
	return br.err
}

type batchResult struct {
	doneC chan struct{}
	err   error
}

type db interface {
	sql.Executor

	WithTx(context.Context, func(sql.Transaction) error) error
}
