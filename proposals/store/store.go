package store

import (
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

var (
	ErrNotFound       = errors.New("proposal not found")
	ErrLayerEvicted   = errors.New("layer evicted")
	ErrProposalExists = errors.New("proposal already exists")
)

type Store struct {
	// number of layers to keep
	capacity types.LayerID
	logger   *zap.Logger

	// guards access to data and evicted
	mu      sync.RWMutex
	evicted *types.LayerID
	data    map[types.LayerID]map[types.ProposalID]*types.Proposal
}

type StoreOption func(*Store)

func WithCapacity(capacity types.LayerID) StoreOption {
	return func(s *Store) {
		s.capacity = capacity
	}
}

func WithFirstLayer(layer types.LayerID) StoreOption {
	return func(s *Store) {
		s.evicted = &layer
	}
}

func WithStoreLogger(logger *zap.Logger) StoreOption {
	return func(s *Store) {
		s.logger = logger
	}
}

func New(opts ...StoreOption) *Store {
	s := &Store{
		data:     make(map[types.LayerID]map[types.ProposalID]*types.Proposal),
		capacity: 2,
		logger:   zap.NewNop(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Store) IsEvicted(layer types.LayerID) bool {
	return s.evicted != nil && *s.evicted >= layer
}

func (s *Store) OnLayer(layer types.LayerID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	evicted := types.LayerID(0)
	if s.evicted != nil {
		evicted = *s.evicted
	}
	if layer >= evicted+s.capacity {
		toEvict := layer - s.capacity
		for lid := evicted; lid <= toEvict; lid++ {
			s.logger.Debug("evicting layer", zap.Uint32("layer", uint32(lid)))
			delete(s.data, lid)
		}
		s.evicted = &toEvict
	}
}

func (s *Store) Add(p *types.Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsEvicted(p.Layer) {
		return ErrLayerEvicted
	}

	if s.data[p.Layer] == nil {
		s.data[p.Layer] = make(map[types.ProposalID]*types.Proposal)
	}

	if _, ok := s.data[p.Layer][p.ID()]; ok {
		return ErrProposalExists
	}
	s.data[p.Layer][p.ID()] = p
	return nil
}

func (s *Store) Get(layer types.LayerID, id types.ProposalID) *types.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.data[layer]; !ok {
		return nil
	}

	return s.data[layer][id]
}

func (s *Store) GetForLayer(layer types.LayerID) []*types.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.IsEvicted(layer) {
		return nil
	}

	if proposals, ok := s.data[layer]; ok {
		return maps.Values(proposals)
	}
	return nil
}

func (s *Store) GetMany(layer types.LayerID, pids ...types.ProposalID) []*types.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.IsEvicted(layer) {
		return nil
	}

	proposals := make([]*types.Proposal, 0, len(pids))
	for _, pid := range pids {
		if p, ok := s.data[layer][pid]; ok {
			proposals = append(proposals, p)
		}
	}
	return proposals
}

func (s *Store) Has(id types.ProposalID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for layer := range s.data {
		if _, ok := s.data[layer][id]; ok {
			return true
		}
	}
	return false
}

func (s *Store) GetBlob(id types.ProposalID) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for layer := range s.data {
		if proposal, ok := s.data[layer][id]; ok {
			blob, err := codec.Encode(proposal)
			if err != nil {
				return nil, fmt.Errorf("encoding proposal: %w", err)
			}
			return blob, nil
		}
	}
	return nil, ErrNotFound
}
