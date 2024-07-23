package dbsync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
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
		require.Equal(t, tc.overflow, id.inc())
		require.Equal(t, tc.expected, id)
	}
}

func createDB(t *testing.T, keyLen int) sql.Database {
	db := sql.InMemory(sql.WithIgnoreSchemaDrift())
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	_, err := db.Exec(fmt.Sprintf("create table foo(id char(%d))", keyLen), nil, nil)
	require.NoError(t, err)
	return db
}

func insertDBItems(t *testing.T, db sql.Database, content []KeyBytes) {
	for _, id := range content {
		_, err := db.Exec(
			"insert into foo(id) values(?)",
			func(stmt *sql.Statement) {
				stmt.BindBytes(1, id)
			}, nil)
		require.NoError(t, err)
	}
}

func deleteDBItems(t *testing.T, db sql.Database) {
	_, err := db.Exec("delete from foo", nil, nil)
	require.NoError(t, err)
}

func populateDB(t *testing.T, keyLen int, content []KeyBytes) sql.Database {
	db := createDB(t, keyLen)
	insertDBItems(t, db, content)
	return db
}

const testQuery = "select id from foo where id >= ? order by id limit ?"

func TestDBRangeIterator(t *testing.T) {
	db := createDB(t, 4)
	for _, tc := range []struct {
		items  []KeyBytes
		from   KeyBytes
		fromN  int
		expErr error
	}{
		{
			items:  nil,
			from:   KeyBytes{0x00, 0x00, 0x00, 0x00},
			expErr: errEmptySet,
		},
		{
			items:  nil,
			from:   KeyBytes{0x80, 0x00, 0x00, 0x00},
			expErr: errEmptySet,
		},
		{
			items:  nil,
			from:   KeyBytes{0xff, 0xff, 0xff, 0xff},
			expErr: errEmptySet,
		},
		{
			items: []KeyBytes{
				{0x00, 0x00, 0x00, 0x00},
			},
			from:  KeyBytes{0x00, 0x00, 0x00, 0x00},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				{0x00, 0x00, 0x00, 0x00},
			},
			from:  KeyBytes{0x01, 0x00, 0x00, 0x00},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				{0x00, 0x00, 0x00, 0x00},
			},
			from:  KeyBytes{0xff, 0xff, 0xff, 0xff},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				{0x01, 0x02, 0x03, 0x04},
			},
			from:  KeyBytes{0x00, 0x00, 0x00, 0x00},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				{0x01, 0x02, 0x03, 0x04},
			},
			from:  KeyBytes{0x01, 0x00, 0x00, 0x00},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				{0x01, 0x02, 0x03, 0x04},
			},
			from:  KeyBytes{0xff, 0xff, 0xff, 0xff},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				{0xff, 0xff, 0xff, 0xff},
			},
			from:  KeyBytes{0x00, 0x00, 0x00, 0x00},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				{0xff, 0xff, 0xff, 0xff},
			},
			from:  KeyBytes{0x01, 0x00, 0x00, 0x00},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				{0xff, 0xff, 0xff, 0xff},
			},
			from:  KeyBytes{0xff, 0xff, 0xff, 0xff},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				0: {0x00, 0x00, 0x00, 0x01},
				1: {0x00, 0x00, 0x00, 0x03},
				2: {0x00, 0x00, 0x00, 0x05},
				3: {0x00, 0x00, 0x00, 0x07},
			},
			from:  KeyBytes{0x00, 0x00, 0x00, 0x00},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				0: {0x00, 0x00, 0x00, 0x01},
				1: {0x00, 0x00, 0x00, 0x03},
				2: {0x00, 0x00, 0x00, 0x05},
				3: {0x00, 0x00, 0x00, 0x07},
			},
			from:  KeyBytes{0x00, 0x00, 0x00, 0x01},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				0: {0x00, 0x00, 0x00, 0x01},
				1: {0x00, 0x00, 0x00, 0x03},
				2: {0x00, 0x00, 0x00, 0x05},
				3: {0x00, 0x00, 0x00, 0x07},
			},
			from:  KeyBytes{0x00, 0x00, 0x00, 0x02},
			fromN: 1,
		},
		{
			items: []KeyBytes{
				0: {0x00, 0x00, 0x00, 0x01},
				1: {0x00, 0x00, 0x00, 0x03},
				2: {0x00, 0x00, 0x00, 0x05},
				3: {0x00, 0x00, 0x00, 0x07},
			},
			from:  KeyBytes{0x00, 0x00, 0x00, 0x03},
			fromN: 1,
		},
		{
			items: []KeyBytes{
				0: {0x00, 0x00, 0x00, 0x01},
				1: {0x00, 0x00, 0x00, 0x03},
				2: {0x00, 0x00, 0x00, 0x05},
				3: {0x00, 0x00, 0x00, 0x07},
			},
			from:  KeyBytes{0x00, 0x00, 0x00, 0x05},
			fromN: 2,
		},
		{
			items: []KeyBytes{
				0: {0x00, 0x00, 0x00, 0x01},
				1: {0x00, 0x00, 0x00, 0x03},
				2: {0x00, 0x00, 0x00, 0x05},
				3: {0x00, 0x00, 0x00, 0x07},
			},
			from:  KeyBytes{0x00, 0x00, 0x00, 0x07},
			fromN: 3,
		},
		{
			items: []KeyBytes{
				0: {0x00, 0x00, 0x00, 0x01},
				1: {0x00, 0x00, 0x00, 0x03},
				2: {0x00, 0x00, 0x00, 0x05},
				3: {0x00, 0x00, 0x00, 0x07},
			},
			from:  KeyBytes{0xff, 0xff, 0xff, 0xff},
			fromN: 0,
		},
		{
			items: []KeyBytes{
				0:  {0x00, 0x00, 0x00, 0x01},
				1:  {0x00, 0x00, 0x00, 0x03},
				2:  {0x00, 0x00, 0x00, 0x05},
				3:  {0x00, 0x00, 0x00, 0x07},
				4:  {0x00, 0x00, 0x01, 0x00},
				5:  {0x00, 0x00, 0x03, 0x00},
				6:  {0x00, 0x01, 0x00, 0x00},
				7:  {0x00, 0x05, 0x00, 0x00},
				8:  {0x03, 0x05, 0x00, 0x00},
				9:  {0x09, 0x05, 0x00, 0x00},
				10: {0x0a, 0x05, 0x00, 0x00},
				11: {0xff, 0xff, 0xff, 0xff},
			},
			from:  KeyBytes{0x00, 0x00, 0x03, 0x01},
			fromN: 6,
		},
		{
			items: []KeyBytes{
				0:  {0x00, 0x00, 0x00, 0x01},
				1:  {0x00, 0x00, 0x00, 0x03},
				2:  {0x00, 0x00, 0x00, 0x05},
				3:  {0x00, 0x00, 0x00, 0x07},
				4:  {0x00, 0x00, 0x01, 0x00},
				5:  {0x00, 0x00, 0x03, 0x00},
				6:  {0x00, 0x01, 0x00, 0x00},
				7:  {0x00, 0x05, 0x00, 0x00},
				8:  {0x03, 0x05, 0x00, 0x00},
				9:  {0x09, 0x05, 0x00, 0x00},
				10: {0x0a, 0x05, 0x00, 0x00},
				11: {0xff, 0xff, 0xff, 0xff},
			},
			from:  KeyBytes{0x00, 0x01, 0x00, 0x00},
			fromN: 6,
		},
		{
			items: []KeyBytes{
				0:  {0x00, 0x00, 0x00, 0x01},
				1:  {0x00, 0x00, 0x00, 0x03},
				2:  {0x00, 0x00, 0x00, 0x05},
				3:  {0x00, 0x00, 0x00, 0x07},
				4:  {0x00, 0x00, 0x01, 0x00},
				5:  {0x00, 0x00, 0x03, 0x00},
				6:  {0x00, 0x01, 0x00, 0x00},
				7:  {0x00, 0x05, 0x00, 0x00},
				8:  {0x03, 0x05, 0x00, 0x00},
				9:  {0x09, 0x05, 0x00, 0x00},
				10: {0x0a, 0x05, 0x00, 0x00},
				11: {0xff, 0xff, 0xff, 0xff},
			},
			from:  KeyBytes{0xff, 0xff, 0xff, 0xff},
			fromN: 11,
		},
	} {
		deleteDBItems(t, db)
		insertDBItems(t, db, tc.items)
		for maxChunkSize := 1; maxChunkSize < 12; maxChunkSize++ {
			it, err := newDBRangeIterator(db, testQuery, tc.from, maxChunkSize)
			if tc.expErr != nil {
				require.ErrorIs(t, err, tc.expErr)
				continue
			}
			require.NoError(t, err)
			// when there are no items, errEmptySet is returned
			require.NotEmpty(t, tc.items)
			clonedIt := it.clone()
			var collected []KeyBytes
			for i := 0; i < len(tc.items); i++ {
				k := it.Key()
				require.NotNil(t, k)
				collected = append(collected, k.(KeyBytes))
				require.Equal(t, k, clonedIt.Key())
				require.NoError(t, it.Next())
				// calling Next on the original iterator
				// shouldn't affect the cloned one
				require.Equal(t, k, clonedIt.Key())
				require.NoError(t, clonedIt.Next())
			}
			expected := slices.Concat(tc.items[tc.fromN:], tc.items[:tc.fromN])
			require.Equal(t, expected, collected, "count=%d from=%s maxChunkSize=%d",
				len(tc.items), hex.EncodeToString(tc.from), maxChunkSize)
			clonedIt = it.clone()
			for range 2 {
				for i := 0; i < len(tc.items); i++ {
					k := it.Key()
					require.Equal(t, collected[i], k.(KeyBytes))
					require.Equal(t, k, clonedIt.Key())
					require.NoError(t, it.Next())
					require.Equal(t, k, clonedIt.Key())
					require.NoError(t, clonedIt.Next())
				}
			}
		}
	}
}

type fakeIterator struct {
	items, allItems []KeyBytes
}

var _ hashsync.Iterator = &fakeIterator{}

func (it *fakeIterator) Key() hashsync.Ordered {
	if len(it.allItems) == 0 {
		panic("no items")
	}
	if len(it.items) == 0 {
		it.items = it.allItems
	}
	return KeyBytes(it.items[0])
}

func (it *fakeIterator) Next() error {
	if len(it.items) == 0 {
		it.items = it.allItems
	}
	it.items = it.items[1:]
	if len(it.items) != 0 && string(it.items[0]) == "error" {
		return errors.New("iterator error")
	}
	return nil
}

func (it *fakeIterator) clone() iterator {
	cloned := &fakeIterator{
		allItems: make([]KeyBytes, len(it.allItems)),
	}
	for i, k := range it.allItems {
		cloned.allItems[i] = slices.Clone(k)
	}
	cloned.items = cloned.allItems[len(it.allItems)-len(it.items):]
	return cloned
}

func TestCombineIterators(t *testing.T) {
	it1 := &fakeIterator{
		allItems: []KeyBytes{
			{0x00, 0x00, 0x00, 0x01},
			{0x0a, 0x05, 0x00, 0x00},
		},
	}
	it2 := &fakeIterator{
		allItems: []KeyBytes{
			{0x00, 0x00, 0x00, 0x03},
			{0xff, 0xff, 0xff, 0xff},
		},
	}

	it := combineIterators(it1, it2)
	clonedIt := it.clone()
	for range 3 {
		var collected []KeyBytes
		for i := 0; i < 4; i++ {
			k := it.Key()
			collected = append(collected, k.(KeyBytes))
			require.Equal(t, k, clonedIt.Key())
			require.NoError(t, it.Next())
			require.Equal(t, k, clonedIt.Key())
			require.NoError(t, clonedIt.Next())
		}
		require.Equal(t, []KeyBytes{
			{0x00, 0x00, 0x00, 0x01},
			{0x00, 0x00, 0x00, 0x03},
			{0x0a, 0x05, 0x00, 0x00},
			{0xff, 0xff, 0xff, 0xff},
		}, collected)
		require.Equal(t, KeyBytes{0x00, 0x00, 0x00, 0x01}, it.Key())
	}

	it1 = &fakeIterator{allItems: []KeyBytes{KeyBytes{0, 0, 0, 0}, KeyBytes("error")}}
	it2 = &fakeIterator{allItems: []KeyBytes{KeyBytes{0, 0, 0, 1}}}

	it = combineIterators(it1, it2)
	require.Equal(t, KeyBytes{0, 0, 0, 0}, it.Key())
	require.Error(t, it.Next())

	it1 = &fakeIterator{allItems: []KeyBytes{KeyBytes{0, 0, 0, 0}}}
	it2 = &fakeIterator{allItems: []KeyBytes{KeyBytes{0, 0, 0, 1}, KeyBytes("error")}}

	it = combineIterators(it1, it2)
	require.Equal(t, KeyBytes{0, 0, 0, 0}, it.Key())
	require.NoError(t, it.Next())
	require.Equal(t, KeyBytes{0, 0, 0, 1}, it.Key())
	require.Error(t, it.Next())
}
