package atxwriter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

var writerDelay = 100 * time.Millisecond

type AtxWriter struct {
	db     db
	logger *zap.Logger

	atxMu          sync.Mutex
	ticker         *time.Ticker
	pool           *sync.Pool
	atxBatch       map[types.ATXID]atxBatchItem
	atxBatchResult *batchResult
}

func New(db db, logger *zap.Logger) *AtxWriter {
	// create a stopped ticker so we could reuse it later on
	ticker := time.NewTicker(writerDelay)
	ticker.Stop()

	writer := &AtxWriter{
		db:     db,
		logger: logger,
		ticker: ticker,
		pool: &sync.Pool{
			New: func() any {
				s := make(map[types.ATXID]atxBatchItem)
				return &s
			},
		},
		atxBatchResult: &batchResult{
			doneC: make(chan struct{}),
		},
	}
	writer.atxBatch = writer.getBatch()
	return writer
}

func (w *AtxWriter) getBatch() map[types.ATXID]atxBatchItem {
	v := w.pool.Get().(*map[types.ATXID]atxBatchItem)
	return *v
}

// putBatch puts back the map into the pool.
func (w *AtxWriter) putBatch(v map[types.ATXID]atxBatchItem) {
	clear(v)
	w.pool.Put(&v)
}

// Start the forever-loop that flushes the atxs to the DB
// at-least every `writerDelay`. The caller is responsible
// to call Start in a different goroutine.
func (w *AtxWriter) Start(ctx context.Context) {
	w.ticker.Reset(writerDelay)
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.ticker.C:
			// copy-on-write
			w.atxMu.Lock()
			if len(w.atxBatch) == 0 {
				w.atxMu.Unlock()
				continue
			}
			batch := w.atxBatch                                         // copy the existing slice
			w.atxBatch = w.getBatch()                                   // make a new one
			res := w.atxBatchResult                                     // copy the result type
			w.atxBatchResult = &batchResult{doneC: make(chan struct{})} // make a new one
			w.atxMu.Unlock()
			start := time.Now()
			BatchWriteCount.Inc()
			FlushBatchSize.Add(float64(len(batch)))

			if err := w.db.WithTx(ctx, func(tx sql.Transaction) error {
				var err error
				for _, item := range batch {
					err = atxs.Add(tx, item.Atx, item.Watx.Blob())
					if err != nil && !errors.Is(err, sql.ErrObjectExists) {
						WriteBatchErrorsCount.Inc()
						return fmt.Errorf("add atx to db: %w", err)
					}
					err = atxs.SetPost(tx, item.Atx.ID(), item.Watx.PrevATXID, 0,
						item.Atx.SmesherID, item.Watx.NumUnits, item.Watx.PublishEpoch)
					if err != nil && !errors.Is(err, sql.ErrObjectExists) {
						WriteBatchErrorsCount.Inc()
						return fmt.Errorf("set post: %w", err)
					}
				}
				return nil
			}); err != nil {
				res.err = err
				ErroredBatchCount.Inc()
				w.logger.Error("flush atxs to db", zap.Error(err))
			}

			tdiff := time.Since(start)
			WriteTime.Add(float64(tdiff))
			AtxWriteTimeHist.Observe(tdiff.Seconds())
			close(res.doneC)
			w.putBatch(batch)
		}
	}
}

func (w *AtxWriter) Store(atx *types.ActivationTx, watx *wire.ActivationTxV1) (<-chan struct{}, func() error) {
	w.atxMu.Lock()
	defer w.atxMu.Unlock()

	w.atxBatch[atx.ID()] = atxBatchItem{Atx: atx, Watx: watx}
	br := w.atxBatchResult
	c := br.doneC
	return c, br.Error
}

type batchResult struct {
	doneC chan struct{}
	err   error
}

func (b *batchResult) Error() error {
	return b.err
}

type atxBatchItem struct {
	Atx  *types.ActivationTx
	Watx *wire.ActivationTxV1
}

type db interface {
	WithTx(context.Context, func(sql.Transaction) error) error
	sql.Executor
}
