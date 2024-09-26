package sql

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	testNumInsert             = 1000
	testFalsePositiveRate     = 0.01
	testNumChecks             = 10000
	testMaxFalsePositiveCount = int(testNumChecks * testFalsePositiveRate * 2)
)

func randomID() []byte {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return b
}

type bfTester struct {
	*testing.T
	bf *DBBloomFilter
	db Database
}

func setUpBloomFilterTest(t *testing.T, sel string) *bfTester {
	db := InMemoryTest(t)
	_, err := db.Exec("CREATE TABLE test (id CHAR(32), include int)", nil, nil)
	require.NoError(t, err)
	bf := NewDBBloomFilter(zaptest.NewLogger(t), "bloomTest",
		sel, "id", testNumInsert, 1.5, testFalsePositiveRate)
	db.AddSet(bf)
	t.Cleanup(bf.Stop)
	return &bfTester{t, bf, db}
}

func (bft *bfTester) insertRandom(include bool) []byte {
	id := randomID()
	_, err := bft.db.Exec("INSERT INTO test (id, include) VALUES (?, ?)", func(st *Statement) {
		st.BindBytes(1, id)
		st.BindBool(2, include)
	}, nil)
	require.NoError(bft, err)
	return id
}

func (bft *bfTester) insertIDs(count int, include bool) [][]byte {
	ids := make([][]byte, count)
	for i := range ids {
		ids[i] = bft.insertRandom(include)
	}
	return ids
}

func (bft *bfTester) start() {
	bft.bf.Start(bft.db)
	require.Eventually(bft, bft.bf.Ready, time.Second, 10*time.Millisecond)
}

func (bft *bfTester) verifyContains(ids [][]byte) {
	for _, id := range ids {
		has, err := Contains(bft.db, "bloomTest", id)
		require.NoError(bft, err)
		require.True(bft, has)
	}
}

func (bft *bfTester) verifyNotContains(ex Executor) {
	c := bft.db.QueryCount()
	for range 10 {
		oldStats := bft.bf.Stats()
		for range testNumChecks {
			id := randomID()
			has, err := Contains(ex, "bloomTest", id)
			require.NoError(bft, err)
			require.False(bft, has)
		}
		count := bft.db.QueryCount() - c
		newStats := bft.bf.Stats()
		require.Equal(bft, count, newStats.NumPositive-oldStats.NumPositive)
		require.Equal(bft, testNumChecks-count, newStats.NumNegative-oldStats.NumNegative)
		bft.Logf("query count: %d, maxFalsePositiveCount: %d", count, testMaxFalsePositiveCount)
		require.GreaterOrEqual(bft, testMaxFalsePositiveCount, count)
		c = bft.db.QueryCount()
	}
}

func TestDBBloomFilter(t *testing.T) {
	bft := setUpBloomFilterTest(t, "select id from test")
	ids := bft.insertIDs(testNumInsert, false)

	c := bft.db.QueryCount()
	bft.start()
	require.Equal(t, BloomStats{
		Loaded: testNumInsert,
	}, bft.bf.Stats())

	c += 2
	require.Equal(t, c, bft.db.QueryCount())
	bft.verifyContains(ids)
	c += testNumInsert
	require.Equal(t, c, bft.db.QueryCount())
	require.Equal(t, BloomStats{
		Loaded:      testNumInsert,
		NumPositive: testNumInsert,
	}, bft.bf.Stats())

	bft.verifyNotContains(bft.db)
	require.NoError(t, bft.db.WithTx(context.Background(), func(tx Transaction) error {
		bft.verifyNotContains(tx)
		return nil
	}))
}

func TestBloomFilter_Add(t *testing.T) {
	bft := setUpBloomFilterTest(t, "select id from test")
	ids := bft.insertIDs(testNumInsert, false)
	bft.start()
	for range 100 {
		newID := bft.insertRandom(false)

		c := bft.db.QueryCount()
		nAdded := bft.bf.Stats().Added
		bft.db.AddToSet("bloomTest", newID)
		require.Eventually(t, func() bool {
			return bft.bf.Stats().Added == nAdded+1
		}, time.Second, 50*time.Microsecond)
		require.True(t, bft.bf.mayHave(newID))
		has, err := Contains(bft.db, "bloomTest", newID)
		require.NoError(t, err)
		require.True(t, has)
		require.Equal(t, c+1, bft.db.QueryCount())
	}
	require.Equal(t, 100, bft.bf.Stats().Added)
	bft.verifyContains(ids)
	bft.verifyNotContains(bft.db)
}

func TestBloomFilter_Where(t *testing.T) {
	const numSkip = 100
	bft := setUpBloomFilterTest(t, "select id from test where include = 1")
	ids := bft.insertIDs(testNumInsert, true)
	skip := bft.insertIDs(numSkip, false)
	bft.start()
	c := bft.db.QueryCount()
	bft.verifyContains(ids)
	c += testNumInsert
	require.Equal(t, c, bft.db.QueryCount())
	require.Equal(t, BloomStats{
		Loaded:      testNumInsert,
		NumPositive: testNumInsert,
	}, bft.bf.Stats())

	bft.verifyNotContains(bft.db)
	for _, id := range skip {
		has, err := Contains(bft.db, "bloomTest", id)
		require.NoError(t, err)
		require.False(t, has, "skipped key included in filter")
	}
}

func TestBloomFilter_NotStarted(t *testing.T) {
	const (
		numInsert = 100
		numCheck  = 100
	)

	bft := setUpBloomFilterTest(t, "select id from test")
	ids := bft.insertIDs(numInsert, false)

	require.False(t, bft.bf.Ready())

	c := bft.db.QueryCount()
	for _, id := range ids {
		has, err := Contains(bft.db, "bloomTest", id)
		require.NoError(t, err)
		require.True(t, has)
	}
	c += numInsert
	require.Equal(t, c, bft.db.QueryCount())

	for range numCheck {
		id := randomID()
		has, err := Contains(bft.db, "bloomTest", id)
		require.NoError(t, err)
		require.False(t, has)
	}
	require.Equal(t, c+numCheck, bft.db.QueryCount())
}

type blockingDB struct {
	Database
	stuck chan struct{}
}

func (db *blockingDB) WithTx(ctx context.Context, exec func(Transaction) error) error {
	<-db.stuck
	<-db.stuck
	return db.Database.WithTx(ctx, exec)
}

func TestBloomFilter_CheckDuringLoad(t *testing.T) {
	const (
		numCheck = 100
	)
	bft := setUpBloomFilterTest(t, "select id from test")
	ids := bft.insertIDs(testNumInsert, false)

	blockingDB := &blockingDB{Database: bft.db, stuck: make(chan struct{})}
	bft.bf.Start(blockingDB)

	// make sure loading has started
	blockingDB.stuck <- struct{}{}

	// check positive results
	for _, id := range ids {
		has, err := Contains(bft.db, "bloomTest", id)
		require.NoError(t, err)
		require.True(t, has)
	}

	// check negative results
	c := bft.db.QueryCount()
	for range numCheck {
		id := randomID()
		has, err := Contains(bft.db, "bloomTest", id)
		require.NoError(t, err)
		require.False(t, has)
	}
	require.Equal(t, c+numCheck, bft.db.QueryCount())

	// let loading continue
	blockingDB.stuck <- struct{}{}
	require.Eventually(bft, bft.bf.Ready, time.Second, 10*time.Millisecond)
	require.Equal(t, BloomStats{
		Loaded: testNumInsert,
	}, bft.bf.Stats())

	// make sure the filter still works and the actual Bloom filter is in use
	bft.verifyContains(ids)
	bft.verifyNotContains(bft.db)
}
