package dbsync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
	"github.com/stretchr/testify/require"
)

func TestIncID(t *testing.T) {
	for _, tc := range []struct {
		id, expected KeyBytes
		overflow     bool
	}{
		{
			id:       KeyBytes{0x00, 0x00, 0x00, 0x00},
			expected: KeyBytes{0x00, 0x00, 0x00, 0x01},
			overflow: false,
		},
		{
			id:       KeyBytes{0x00, 0x00, 0x00, 0xff},
			expected: KeyBytes{0x00, 0x00, 0x01, 0x00},
			overflow: false,
		},
		{
			id:       KeyBytes{0xff, 0xff, 0xff, 0xff},
			expected: KeyBytes{0x00, 0x00, 0x00, 0x00},
			overflow: true,
		},
	} {
		id := make(KeyBytes, len(tc.id))
		copy(id, tc.id)
		require.Equal(t, tc.overflow, incID(id))
		require.Equal(t, tc.expected, id)
	}
}

func populateDB(t *testing.T, keyLen int, content []KeyBytes) sql.Database {
	db := sql.InMemory(sql.WithIgnoreSchemaDrift())
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	_, err := db.Exec(fmt.Sprintf("create table foo(id char(%d))", keyLen), nil, nil)
	require.NoError(t, err)
	for _, id := range content {
		_, err := db.Exec(
			"insert into foo(id) values(?)",
			func(stmt *sql.Statement) {
				stmt.BindBytes(1, id)
			}, nil)
		require.NoError(t, err)
	}
	return db
}

const testQuery = "select id from foo where id between ? and ? order by id limit ?"

func TestDBRangeIterator(t *testing.T) {
	db := populateDB(t, 4, []KeyBytes{
		{0x00, 0x00, 0x00, 0x01},
		{0x00, 0x00, 0x00, 0x03},
		{0x00, 0x00, 0x00, 0x05},
		{0x00, 0x00, 0x00, 0x07},
		{0x00, 0x00, 0x01, 0x00},
		{0x00, 0x00, 0x03, 0x00},
		{0x00, 0x01, 0x00, 0x00},
		{0x00, 0x05, 0x00, 0x00},
		{0x03, 0x05, 0x00, 0x00},
		{0x09, 0x05, 0x00, 0x00},
		{0x0a, 0x05, 0x00, 0x00},
		{0xff, 0xff, 0xff, 0xff},
	})
	for _, tc := range []struct {
		from, to  KeyBytes
		chunkSize int
		items     []KeyBytes
	}{
		{
			from:      KeyBytes{0x00, 0x00, 0x00, 0x00},
			to:        KeyBytes{0x00, 0x00, 0x00, 0x00},
			chunkSize: 4,
			items:     nil,
		},
		{
			from:      KeyBytes{0x00, 0x00, 0x00, 0x00},
			to:        KeyBytes{0x00, 0x00, 0x00, 0x08},
			chunkSize: 4,
			items: []KeyBytes{
				{0x00, 0x00, 0x00, 0x01},
				{0x00, 0x00, 0x00, 0x03},
				{0x00, 0x00, 0x00, 0x05},
				{0x00, 0x00, 0x00, 0x07},
			},
		},
		{
			from:      KeyBytes{0x00, 0x00, 0x00, 0x00},
			to:        KeyBytes{0x00, 0x00, 0x03, 0x00},
			chunkSize: 4,
			items: []KeyBytes{
				{0x00, 0x00, 0x00, 0x01},
				{0x00, 0x00, 0x00, 0x03},
				{0x00, 0x00, 0x00, 0x05},
				{0x00, 0x00, 0x00, 0x07},
				{0x00, 0x00, 0x01, 0x00},
				{0x00, 0x00, 0x03, 0x00},
			},
		},
		{
			from:      KeyBytes{0x00, 0x00, 0x03, 0x00},
			to:        KeyBytes{0x09, 0x05, 0x00, 0x00},
			chunkSize: 4,
			items: []KeyBytes{
				{0x00, 0x00, 0x03, 0x00},
				{0x00, 0x01, 0x00, 0x00},
				{0x00, 0x05, 0x00, 0x00},
				{0x03, 0x05, 0x00, 0x00},
				{0x09, 0x05, 0x00, 0x00},
			},
		},
		{
			from:      KeyBytes{0x00, 0x00, 0x00, 0x00},
			to:        KeyBytes{0xff, 0xff, 0xff, 0xff},
			chunkSize: 4,
			items: []KeyBytes{
				{0x00, 0x00, 0x00, 0x01},
				{0x00, 0x00, 0x00, 0x03},
				{0x00, 0x00, 0x00, 0x05},
				{0x00, 0x00, 0x00, 0x07},
				{0x00, 0x00, 0x01, 0x00},
				{0x00, 0x00, 0x03, 0x00},
				{0x00, 0x01, 0x00, 0x00},
				{0x00, 0x05, 0x00, 0x00},
				{0x03, 0x05, 0x00, 0x00},
				{0x09, 0x05, 0x00, 0x00},
				{0x0a, 0x05, 0x00, 0x00},
				{0xff, 0xff, 0xff, 0xff},
			},
		},
	} {
		it, err := newDBRangeIterator(db, testQuery, tc.from, tc.to, tc.chunkSize)
		require.NoError(t, err)
		if len(tc.items) == 0 {
			require.Nil(t, it.Key())
		} else {
			var collected []KeyBytes
			for i := 0; i < len(tc.items); i++ {
				if k := it.Key(); k != nil {
					collected = append(collected, k.(KeyBytes))
				} else {
					break
				}
				require.NoError(t, it.Next())
			}
			require.Nil(t, it.Key())
			require.Equal(t, tc.items, collected, "from=%s to=%s chunkSize=%d",
				hex.EncodeToString(tc.from), hex.EncodeToString(tc.to), tc.chunkSize)
		}
	}
}

type fakeIterator struct {
	items []KeyBytes
}

var _ hashsync.Iterator = &fakeIterator{}

func (it *fakeIterator) Key() hashsync.Ordered {
	if len(it.items) == 0 {
		return nil
	}
	return KeyBytes(it.items[0])
}

func (it *fakeIterator) Next() error {
	if len(it.items) != 0 {
		it.items = it.items[1:]
	}
	if len(it.items) != 0 && string(it.items[0]) == "error" {
		return errors.New("iterator error")
	}
	return nil
}

func TestConcatIterators(t *testing.T) {
	it1 := &fakeIterator{
		items: []KeyBytes{
			{0x00, 0x00, 0x00, 0x01},
			{0x00, 0x00, 0x00, 0x03},
		},
	}
	it2 := &fakeIterator{
		items: []KeyBytes{
			{0x0a, 0x05, 0x00, 0x00},
			{0xff, 0xff, 0xff, 0xff},
		},
	}

	it := concatIterators(it1, it2)
	var collected []KeyBytes
	for i := 0; i < 4; i++ {
		collected = append(collected, it.Key().(KeyBytes))
		require.NoError(t, it.Next())
	}
	require.Nil(t, it.Key())
	require.Equal(t, []KeyBytes{
		{0x00, 0x00, 0x00, 0x01},
		{0x00, 0x00, 0x00, 0x03},
		{0x0a, 0x05, 0x00, 0x00},
		{0xff, 0xff, 0xff, 0xff},
	}, collected)

	it1 = &fakeIterator{items: []KeyBytes{KeyBytes{0, 0, 0, 0}, KeyBytes("error")}}
	it2 = &fakeIterator{items: nil}

	it = concatIterators(it1, it2)
	require.Equal(t, KeyBytes{0, 0, 0, 0}, it.Key())
	require.Error(t, it.Next())

	it1 = &fakeIterator{items: []KeyBytes{KeyBytes{0, 0, 0, 0}}}
	it2 = &fakeIterator{items: []KeyBytes{KeyBytes{0, 0, 0, 1}, KeyBytes("error")}}

	it = concatIterators(it1, it2)
	require.Equal(t, KeyBytes{0, 0, 0, 0}, it.Key())
	require.NoError(t, it.Next())
	require.Equal(t, KeyBytes{0, 0, 0, 1}, it.Key())
	require.Error(t, it.Next())
}

func TestCombineIterators(t *testing.T) {
	it1 := &fakeIterator{
		items: []KeyBytes{
			{0x00, 0x00, 0x00, 0x01},
			{0x0a, 0x05, 0x00, 0x00},
		},
	}
	it2 := &fakeIterator{
		items: []KeyBytes{
			{0x00, 0x00, 0x00, 0x03},
			{0xff, 0xff, 0xff, 0xff},
		},
	}

	it := combineIterators(it1, it2)
	var collected []KeyBytes
	for i := 0; i < 4; i++ {
		collected = append(collected, it.Key().(KeyBytes))
		require.NoError(t, it.Next())
	}
	require.Nil(t, it.Key())
	require.Equal(t, []KeyBytes{
		{0x00, 0x00, 0x00, 0x01},
		{0x00, 0x00, 0x00, 0x03},
		{0x0a, 0x05, 0x00, 0x00},
		{0xff, 0xff, 0xff, 0xff},
	}, collected)

	it1 = &fakeIterator{items: []KeyBytes{KeyBytes{0, 0, 0, 0}, KeyBytes("error")}}
	it2 = &fakeIterator{items: nil}

	it = combineIterators(it1, it2)
	require.Equal(t, KeyBytes{0, 0, 0, 0}, it.Key())
	require.Error(t, it.Next())

	it1 = &fakeIterator{items: []KeyBytes{KeyBytes{0, 0, 0, 0}}}
	it2 = &fakeIterator{items: []KeyBytes{KeyBytes{0, 0, 0, 1}, KeyBytes("error")}}

	it = combineIterators(it1, it2)
	require.Equal(t, KeyBytes{0, 0, 0, 0}, it.Key())
	require.NoError(t, it.Next())
	require.Equal(t, KeyBytes{0, 0, 0, 1}, it.Key())
	require.Error(t, it.Next())
}
