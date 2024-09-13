package sql

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/bits-and-blooms/bloom/v3"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/sql/expr"
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
	mtx       sync.Mutex
	f         *bloom.BloomFilter
	name      string
	sel       expr.Statement
	idCol     expr.Expr
	minSize   int
	fp        float64
	extraCoef float64
	added     atomic.Int64
	loaded    atomic.Int64
	positive  atomic.Int64
	negative  atomic.Int64
}

var _ IDSet = &DBBloomFilter{}

// NewDBBloomFilter creates a new Bloom filter that for a database table.
// tableName is the name of the table, idColumn is the name of the column that contains
// the IDs, filter is an optional SQL expression that selects the rows to include in the
// filter, and falsePositiveRate is the desired false positive rate.
func NewDBBloomFilter(
	name, selectExpr, idCol string,
	minSize int,
	extraCoef, falsePositiveRate float64,
) *DBBloomFilter {
	return &DBBloomFilter{
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
	bf.mtx.Lock()
	defer bf.mtx.Unlock()
	if bf.f != nil {
		bf.f.Add(id)
		bf.added.Add(1)
	}
}

// Load populates the Bloom filter from the database.
func (bf *DBBloomFilter) Load(db Executor, logger *zap.Logger) error {
	bf.mtx.Lock()
	defer bf.mtx.Unlock()
	if bf.f != nil {
		return nil
	}
	logger.Info("estimating Bloom filter size", zap.String("table", bf.name))
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
	bf.f = bloom.NewWithEstimates(uint(size), bf.fp)
	logger.Info("loading Bloom filter",
		zap.String("table", bf.name),
		zap.Int("count", count),
		zap.Int("actualSize", size),
		zap.Int("bytes", bf.f.BitSet().BinaryStorageSize()),
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
		bf.f.Add(bs)
		bf.loaded.Add(1)
		return true
	})
	if err != nil {
		return fmt.Errorf("populate Bloom filter for the table %s: %w", bf.name, err)
	}
	logger.Info("done loading Bloom filter", zap.String("table", bf.name), zap.Int("rows", nRows))
	return nil
}

func (bf *DBBloomFilter) mayHave(id []byte) bool {
	bf.mtx.Lock()
	defer bf.mtx.Unlock()
	if bf.f == nil {
		return false
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

// Contains returns true if the ID is in the table.
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
