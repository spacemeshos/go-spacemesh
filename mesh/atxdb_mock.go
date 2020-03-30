package mesh

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type AtxDbMock struct {
	db      map[types.ATXID]*types.ActivationTx
	nipsts  map[types.ATXID]*types.NIPST
	ProcCnt int
}

func NewAtxDbMock() *AtxDbMock {
	return &AtxDbMock{
		db:     make(map[types.ATXID]*types.ActivationTx),
		nipsts: make(map[types.ATXID]*types.NIPST),
	}
}

func (t *AtxDbMock) GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	if id == *types.EmptyATXID {
		return nil, fmt.Errorf("trying to fetch empty atx id")
	}

	if atx, ok := t.db[id]; ok {
		return atx.ActivationTxHeader, nil
	}
	return nil, fmt.Errorf("cannot find atx")
}

func (t *AtxDbMock) GetATXs(atxIds []types.ATXID) (map[types.ATXID]*types.ActivationTx, []types.ATXID) {
	return nil, nil
}

func (t *AtxDbMock) GetFullAtx(id types.ATXID) (*types.ActivationTx, error) {
	return t.db[id], nil
}

func (t *AtxDbMock) AddAtx(id types.ATXID, atx *types.ActivationTx) {
	t.db[id] = atx
	t.nipsts[id] = atx.Nipst
}

func (t *AtxDbMock) ProcessAtxs(atxs []*types.ActivationTx) error {
	t.ProcCnt += len(atxs)
	return nil
}

func (AtxDbMock) SyntacticallyValidateAtx(atx *types.ActivationTx) error {
	return nil
}
