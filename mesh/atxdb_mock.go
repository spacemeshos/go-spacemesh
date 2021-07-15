package mesh

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

// AtxDbMock is a mock of an activation DB
type AtxDbMock struct {
	db      map[types.ATXID]*types.ActivationTx
	niposts map[types.ATXID]*types.NIPost
	ProcCnt int
}

// NewAtxDbMock returns a new AtxDbMock
func NewAtxDbMock() *AtxDbMock {
	return &AtxDbMock{
		db:      make(map[types.ATXID]*types.ActivationTx),
		niposts: make(map[types.ATXID]*types.NIPost),
	}
}

// GetAtxHeader returns a new ActivationTxHeader
func (t *AtxDbMock) GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	if id == *types.EmptyATXID {
		return nil, fmt.Errorf("trying to fetch empty atx id")
	}

	if atx, ok := t.db[id]; ok {
		return atx.ActivationTxHeader, nil
	}
	return nil, fmt.Errorf("cannot find atx")
}

// GetFullAtx returns a full ATX
func (t *AtxDbMock) GetFullAtx(id types.ATXID) (*types.ActivationTx, error) {
	return t.db[id], nil
}

// AddAtx stores an ATX for later retrieval
func (t *AtxDbMock) AddAtx(id types.ATXID, atx *types.ActivationTx) {
	t.db[id] = atx
	t.niposts[id] = atx.NIPost
}

// ProcessAtxs counts how many ATXs were processed
func (t *AtxDbMock) ProcessAtxs(atxs []*types.ActivationTx) error {
	t.ProcCnt += len(atxs)
	return nil
}

// SyntacticallyValidateAtx always returns no error
func (AtxDbMock) SyntacticallyValidateAtx(*types.ActivationTx) error {
	return nil
}
