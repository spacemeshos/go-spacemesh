package sql

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/bits-and-blooms/bloom/v3"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/sql/expr"
)

const (
	// This is how many keys DBBloomFilter.Add() will queue before blocking while the
	// filter is being loaded. Not many keys are expected during this time b/c not
	// many new records are usually added during several initial minutes after
	// go-spacemesh startup.
	addKeyChSize = 1000
)

// BloomStats represents bloom filter statistics.
type BloomStats struct {
	Loaded      int
	Added       int
	NumPositive int
	NumNegative int
}

// DBBloomFilter reduces the number of database lookups for the keys that are not in the
// database.
type DBBloomFilter struct {
	logger    *zap.Logger
	name      string
	mtx       sync.Mutex
	f         *bloom.BloomFilter
	sel       expr.Statement
	idCol     expr.Expr
	minSize   int
	fp        float64
	extraCoef float64
	added     atomic.Int64
	loaded    atomic.Int64
	positive  atomic.Int64
	negative  atomic.Int64
	loadOnce  sync.Once
	eg        errgroup.Group
	cancel    context.CancelFunc
	addKeyCh  chan []byte
}

var _ IDSet = &DBBloomFilter{}

// NewDBBloomFilter creates a new Bloom filter that for a database table.
// tableName is the name of the table, idColumn is the name of the column that contains
// the IDs, filter is an optional SQL expression that selects the rows to include in the
// filter, and falsePositiveRate is the desired false positive rate.
func NewDBBloomFilter(
	logger *zap.Logger,
	name, selectExpr, idCol string,
	minSize int,
	extraCoef, falsePositiveRate float64,
) *DBBloomFilter {
	return &DBBloomFilter{
		logger:    logger,
		name:      name,
		sel:       expr.MustParseStatement(selectExpr),
		idCol:     expr.MustParse(idCol),
		minSize:   minSize,
		extraCoef: extraCoef,
		fp:        falsePositiveRate,
	}
}

func (bf *DBBloomFilter) Name() string {
	return bf.name
}

func (bf *DBBloomFilter) countSQL() string {
	return expr.SelectBasedOn(bf.sel).Columns(expr.CountStar()).String()
}

func (bf *DBBloomFilter) loadSQL() string {
	return bf.sel.String()
}

func (bf *DBBloomFilter) hasSQL() string {
	return expr.SelectBasedOn(bf.sel).
		Where(expr.MaybeAnd(
			expr.WhereExpr(bf.sel),
			expr.Op(bf.idCol, expr.EQ, expr.Bind()))).
		String()
}

// Add adds the specified key to the Bloom filter.
func (bf *DBBloomFilter) Add(id []byte) {
	if bf.addKeyCh != nil {
		bf.addKeyCh <- id
	}
}

// Ready returns true if the Bloom filter is started and has finished loading.
func (bf *DBBloomFilter) Ready() bool {
	bf.mtx.Lock()
	defer bf.mtx.Unlock()
	return bf.f != nil
}

func (bf *DBBloomFilter) doLoad(db Executor) error {
	bf.logger.Info("estimating Bloom filter size", zap.String("name", bf.name))
	count := 0
	_, err := db.Exec(bf.countSQL(), nil, func(stmt *Statement) bool {
		count = stmt.ColumnInt(0)
		return true
	})
	if err != nil {
		return fmt.Errorf("get count of table %s: %w", bf.name, err)
	}
	size := int(math.Ceil(float64(count) * bf.extraCoef))
	if bf.minSize > 0 && size < bf.minSize {
		size = bf.minSize
	}
	f := bloom.NewWithEstimates(uint(size), bf.fp)
	bf.logger.Info("loading Bloom filter",
		zap.String("name", bf.name),
		zap.Int("count", count),
		zap.Int("actualSize", size),
		zap.Int("bytes", f.BitSet().BinaryStorageSize()),
		zap.Float64("falsePositiveRate", bf.fp))
	var bs []byte
	nRows, err := db.Exec(bf.loadSQL(), nil, func(stmt *Statement) bool {
		l := stmt.ColumnLen(0)
		if cap(bs) < l {
			bs = make([]byte, l)
		} else {
			bs = bs[:l]
		}
		stmt.ColumnBytes(0, bs)
		f.Add(bs)
		bf.loaded.Add(1)
		return true
	})
	if err != nil {
		return fmt.Errorf("populate Bloom filter for the table %s: %w", bf.name, err)
	}
	bf.mtx.Lock()
	bf.f = f
	bf.mtx.Unlock()
	bf.logger.Info("done loading Bloom filter", zap.String("name", bf.name), zap.Int("rows", nRows))
	return nil
}

// Start starts populating the Bloom filter from the database, and after that starts
// updating it with new keys.  Once the filter is populated, it begins to be used during
// Contains() calls. Before the filter is ready, Contains() falls back to the database
// query.
// Subsequent calls to Start() are no-ops.
func (bf *DBBloomFilter) Start(db Database) {
	bf.loadOnce.Do(func() {
		var ctx context.Context
		ctx, bf.cancel = context.WithCancel(context.Background())
		bf.addKeyCh = make(chan []byte, addKeyChSize)
		bf.eg.Go(func() error {
			if err := db.WithTx(ctx, func(tx Transaction) error {
				return bf.doLoad(tx)
			}); err != nil {
				bf.logger.Error("failed to load Bloom filter",
					zap.String("name", bf.name),
					zap.Error(err))
			}
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case id := <-bf.addKeyCh:
					bf.mtx.Lock()
					bf.f.Add(id)
					bf.mtx.Unlock()
					bf.added.Add(1)
				}
			}
		})
	})
}

// Stop stops loading the Bloom filter handler goroutine, if it's running.
// Subsequent calls to Stop() are no-ops.
func (bf *DBBloomFilter) Stop() {
	if bf.cancel != nil {
		bf.cancel()
		bf.eg.Wait()
		bf.cancel = nil
	}
}

func (bf *DBBloomFilter) mayHave(id []byte) bool {
	if !bf.Ready() {
		return true
	}
	if bf.f.Test(id) {
		bf.positive.Add(1)
		return true
	}
	bf.negative.Add(1)
	return false
}

func (bf *DBBloomFilter) inDB(db Executor, id []byte) (bool, error) {
	nRows, err := db.Exec(bf.hasSQL(), func(stmt *Statement) {
		stmt.BindBytes(1, id)
	}, nil)
	if err != nil {
		return false, fmt.Errorf("check if ID exists in table %s: %w", bf.name, err)
	}
	return nRows != 0, nil
}

// Contains returns true if the ID is in the table. It uses the Bloom filter to reduce the
// number of database lookups after the filter has finished loading, and falls back to the
// database query before that.
func (bf *DBBloomFilter) Contains(db Executor, id []byte) (bool, error) {
	if !bf.mayHave(id) {
		// no false negatives in the Bloom filter
		return false, nil
	}
	return bf.inDB(db, id)
}

// Stats returns Bloom filter statistics.
func (bf *DBBloomFilter) Stats() BloomStats {
	return BloomStats{
		Loaded:      int(bf.loaded.Load()),
		Added:       int(bf.added.Load()),
		NumPositive: int(bf.positive.Load()),
		NumNegative: int(bf.negative.Load()),
	}
}
