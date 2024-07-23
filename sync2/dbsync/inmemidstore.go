package dbsync

import (
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
	"github.com/spacemeshos/go-spacemesh/sync2/internal/skiplist"
)

type inMemIDStore struct {
	sl       *skiplist.SkipList
	keyLen   int
	maxDepth int
	len      int
}

var _ idStore = &inMemIDStore{}

func newInMemIDStore(keyLen, maxDepth int) *inMemIDStore {
	return &inMemIDStore{
		sl:       skiplist.New(keyLen),
		keyLen:   keyLen,
		maxDepth: maxDepth,
	}
}

func (s *inMemIDStore) clone() idStore {
	newStore := newInMemIDStore(s.keyLen, s.maxDepth)
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

func (s *inMemIDStore) iter(from KeyBytes) (iterator, error) {
	node := s.sl.FindGTENode(from)
	if node == nil {
		return nil, errEmptySet
	}
	return &inMemIDStoreIterator{sl: s.sl, node: node}, nil
}

type inMemIDStoreIterator struct {
	sl   *skiplist.SkipList
	node *skiplist.Node
}

var _ iterator = &inMemIDStoreIterator{}

func (it *inMemIDStoreIterator) Key() hashsync.Ordered {
	return KeyBytes(it.node.Key())
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

func (it *inMemIDStoreIterator) clone() iterator {
	cloned := *it
	return &cloned
}
