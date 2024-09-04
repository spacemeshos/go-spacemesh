package dbsync

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/sync2/internal/skiplist"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
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

func (s *inMemIDStore) registerHash(h types.KeyBytes) error {
	s.sl.Add(h)
	s.len++
	return nil
}

func (s *inMemIDStore) all(ctx context.Context) (types.Seq, error) {
	return func(yield func(types.Ordered, error) bool) {
		if s.sl.First() == nil {
			return
		}
		for node := s.sl.First(); ; node = node.Next() {
			if node == nil {
				node = s.sl.First()
			}
			if !yield(types.KeyBytes(node.Key()), nil) {
				return
			}
		}
	}, nil
}

func (s *inMemIDStore) from(ctx context.Context, from types.KeyBytes) (types.Seq, error) {
	return func(yield func(types.Ordered, error) bool) {
		node := s.sl.FindGTENode(from)
		if node == nil {
			node = s.sl.First()
			if node == nil {
				return
			}
		}
		for {
			if !yield(types.KeyBytes(node.Key()), nil) {
				return
			}
			node = node.Next()
			if node == nil {
				node = s.sl.First()
			}
		}
	}, nil
}
