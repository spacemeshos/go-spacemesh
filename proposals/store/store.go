package store

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
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
	data    map[types.LayerID]layerData
}

type layerData struct {
	proposals map[types.ProposalID]*types.Proposal
	metric    prometheus.Counter
}

type StoreOption func(*Store)

func WithCapacity(capacity uint32) StoreOption {
	return func(s *Store) {
		s.capacity = types.LayerID(capacity)
	}
}

func WithEvictedLayer(layer types.LayerID) StoreOption {
	return func(s *Store) {
		s.evicted = &layer
	}
}

func WithLogger(logger *zap.Logger) StoreOption {
	return func(s *Store) {
		s.logger = logger
	}
}

func New(opts ...StoreOption) *Store {
	s := &Store{
		data:     make(map[types.LayerID]layerData),
		capacity: 2,
		logger:   zap.NewNop(),
	}
	for _, opt := range opts {
		opt(s)
	}
	var evicted uint32
	if s.evicted != nil {
		evicted = s.evicted.Uint32()
	}
	s.logger.Info("proposals store created",
		zap.Uint32("capacity", s.capacity.Uint32()),
		zap.Uint32("evicted", evicted),
	)
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
			numProposals.DeleteLabelValues(lid.String())
			delete(s.data, lid)
		}
		s.evicted = &toEvict
	}
}

func (s *Store) Add(p *types.Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsEvicted(p.Layer) {
		s.logger.Debug("can't add proposal in evicted layer",
			zap.Uint32("layer", p.Layer.Uint32()),
			zap.Uint32("evicted", s.evicted.Uint32()),
		)
		return ErrLayerEvicted
	}

	if _, exists := s.data[p.Layer]; !exists {
		s.data[p.Layer] = layerData{
			proposals: make(map[types.ProposalID]*types.Proposal),
			metric:    numProposals.WithLabelValues(p.Layer.String()),
		}
	}

	if _, ok := s.data[p.Layer].proposals[p.ID()]; ok {
		return ErrProposalExists
	}
	s.data[p.Layer].proposals[p.ID()] = p
	s.data[p.Layer].metric.Inc()
	return nil
}

func (s *Store) Get(layer types.LayerID, id types.ProposalID) *types.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.data[layer]; !ok {
		return nil
	}

	return s.data[layer].proposals[id]
}

func (s *Store) GetForLayer(layer types.LayerID) []*types.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if d, ok := s.data[layer]; ok {
		return maps.Values(d.proposals)
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
		if p, ok := s.data[layer].proposals[pid]; ok {
			proposals = append(proposals, p)
		}
	}
	return proposals
}

func (s *Store) Has(id types.ProposalID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for layer := range s.data {
		if _, ok := s.data[layer].proposals[id]; ok {
			return true
		}
	}
	return false
}

func (s *Store) getByID(id types.ProposalID) (*types.Proposal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.data {
		if p, ok := m.proposals[id]; ok {
			return p, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) GetBlobSize(id types.ProposalID) (int, error) {
	p, err := s.getByID(id)
	if err != nil {
		return 0, err
	}
	n, err := codec.EncodeTo(io.Discard, p)
	if err != nil {
		return 0, fmt.Errorf("encoding proposal: %w", err)
	}
	return n, err
}

func (s *Store) GetBlob(id types.ProposalID) ([]byte, error) {
	p, err := s.getByID(id)
	if err != nil {
		return nil, err
	}

	blob, err := codec.Encode(p)
	if err != nil {
		return nil, fmt.Errorf("encoding proposal: %w", err)
	}
	return blob, nil
}
