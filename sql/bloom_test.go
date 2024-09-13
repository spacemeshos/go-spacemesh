package sql

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func randomID() []byte {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return b
}

func TestDBBloomFilter(t *testing.T) {
	const (
		numInsert             = 1000
		falsePositiveRate     = 0.01
		numChecks             = 10000
		maxFalsePositiveCount = int(numChecks * falsePositiveRate * 2)
	)

	db := InMemoryTest(t)
	_, err := db.Exec("CREATE TABLE test (id CHAR(32))", nil, nil)
	require.NoError(t, err)
	ids := make([][]byte, numInsert)
	for i := range ids {
		ids[i] = randomID()
		_, err := db.Exec("INSERT INTO test (id) VALUES (?)", func(st *Statement) {
			st.BindBytes(1, ids[i])
		}, nil)
		require.NoError(t, err)
	}

	c := db.QueryCount()
	bf := NewDBBloomFilter("bloomTest", "select id from test", "id", numInsert, 1.5, falsePositiveRate)
	require.NoError(t, bf.Load(db, zaptest.NewLogger(t)))
	db.AddSet(bf)
	require.Equal(t, BloomStats{
		Loaded: numInsert,
	}, bf.Stats())

	c += 2
	require.Equal(t, c, db.QueryCount())
	for _, id := range ids {
		has, err := Contains(db, "bloomTest", id)
		require.NoError(t, err)
		require.True(t, has)
	}
	c += numInsert
	require.Equal(t, c, db.QueryCount())
	require.Equal(t, BloomStats{
		Loaded:      numInsert,
		NumPositive: numInsert,
	}, bf.Stats())

	check := func(ex Executor) {
		for range 10 {
			oldStats := bf.Stats()
			for range numChecks {
				id := randomID()
				has, err := Contains(ex, "bloomTest", id)
				require.NoError(t, err)
				require.False(t, has)
			}
			count := db.QueryCount() - c
			newStats := bf.Stats()
			require.Equal(t, count, newStats.NumPositive-oldStats.NumPositive)
			require.Equal(t, numChecks-count, newStats.NumNegative-oldStats.NumNegative)
			t.Logf("query count: %d, maxFalsePositiveCount: %d", count, maxFalsePositiveCount)
			require.GreaterOrEqual(t, maxFalsePositiveCount, count)
			c = db.QueryCount()
		}
	}
	check(db)
	require.NoError(t, db.WithTx(context.Background(), func(tx Transaction) error {
		check(tx)
		return nil
	}))

	for range 100 {
		newID := randomID()
		_, err = db.Exec("INSERT INTO test (id) VALUES (?)", func(st *Statement) {
			st.BindBytes(1, newID)
		}, nil)
		require.NoError(t, err)

		c = db.QueryCount()
		db.AddToSet("bloomTest", newID)
		require.True(t, bf.mayHave(newID))
		has, err := Contains(db, "bloomTest", newID)
		require.NoError(t, err)
		require.True(t, has)
		require.Equal(t, c+1, db.QueryCount())
	}
	require.Equal(t, 100, bf.Stats().Added)
}

func TestDBBloomFilterWhere(t *testing.T) {
	const (
		numInsert             = 1000
		numSkip               = 100
		falsePositiveRate     = 0.01
		numChecks             = 10000
		maxFalsePositiveCount = int(numChecks * falsePositiveRate * 2)
	)

	db := InMemoryTest(t, WithConnections(10))
	_, err := db.Exec("CREATE TABLE test (id CHAR(32), include int)", nil, nil)
	require.NoError(t, err)
	ids := make([][]byte, numInsert)
	for i := range ids {
		ids[i] = randomID()
		_, err := db.Exec("INSERT INTO test (id, include) VALUES (?, 1)", func(st *Statement) {
			st.BindBytes(1, ids[i])
		}, nil)
		require.NoError(t, err)
	}
	skip := make([][]byte, numSkip)
	for i := range skip {
		skip[i] = randomID()
		_, err := db.Exec("INSERT INTO test (id, include) VALUES (?, 0)", func(st *Statement) {
			st.BindBytes(1, skip[i])
		}, nil)
		require.NoError(t, err)
	}

	c := db.QueryCount()
	bf := NewDBBloomFilter("bloomTest", "select id from test where include = 1", "id",
		numInsert, 1.5, falsePositiveRate)
	require.NoError(t, bf.Load(db, zaptest.NewLogger(t)))
	db.AddSet(bf)

	c += 2
	require.Equal(t, c, db.QueryCount())
	for _, id := range ids {
		has, err := Contains(db, "bloomTest", id)
		require.NoError(t, err)
		require.True(t, has)
	}
	c += numInsert
	require.Equal(t, c, db.QueryCount())

	for range 5 {
		for range numChecks {
			id := randomID()
			has, err := Contains(db, "bloomTest", id)
			require.NoError(t, err)
			require.False(t, has)
		}
		count := db.QueryCount() - c
		t.Logf("query count: %d, maxFalsePositiveCount: %d", count, maxFalsePositiveCount)
		require.GreaterOrEqual(t, maxFalsePositiveCount, count)
		c = db.QueryCount()
	}

	for _, id := range skip {
		has, err := Contains(db, "bloomTest", id)
		require.NoError(t, err)
		require.False(t, has, "skipped key included in filter")
	}
}
