package dbsync

import (
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
	"github.com/spacemeshos/go-spacemesh/sync2/internal/skiplist"
)

type inMemIDStore struct {
	sl     *skiplist.SkipList
	keyLen int
	len    int
}

var _ idStore = &inMemIDStore{}

func newInMemIDStore(keyLen int) *inMemIDStore {
	return &inMemIDStore{
		sl:     skiplist.New(keyLen),
		keyLen: keyLen,
	}
}

func (s *inMemIDStore) clone() idStore {
	newStore := newInMemIDStore(s.keyLen)
	for node := s.sl.First(); node != nil; node = node.Next() {
		newStore.sl.Add(node.Key())
	}
	return newStore
}

func (s *inMemIDStore) registerHash(h KeyBytes) error {
	s.sl.Add(h)
	s.len++
	return nil
}

func (s *inMemIDStore) start() hashsync.Iterator {
	return &inMemIDStoreIterator{sl: s.sl, node: s.sl.First()}
}

func (s *inMemIDStore) iter(from KeyBytes) hashsync.Iterator {
	node := s.sl.FindGTENode(from)
	if node == nil {
		node = s.sl.First()
	}
	return &inMemIDStoreIterator{sl: s.sl, node: node}
}

type inMemIDStoreIterator struct {
	sl   *skiplist.SkipList
	node *skiplist.Node
}

var _ hashsync.Iterator = &inMemIDStoreIterator{}

func (it *inMemIDStoreIterator) Key() (hashsync.Ordered, error) {
	if it.node == nil {
		return nil, errEmptySet
	}
	return KeyBytes(it.node.Key()), nil
}

func (it *inMemIDStoreIterator) Next() error {
	if it.node = it.node.Next(); it.node == nil {
		it.node = it.sl.First()
		if it.node == nil {
			panic("BUG: iterator returned for an empty skiplist")
		}
	}
	return nil
}

func (it *inMemIDStoreIterator) Clone() hashsync.Iterator {
	cloned := *it
	return &cloned
}
