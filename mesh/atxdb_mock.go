package mesh

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type AtxDbMock struct {
	db      map[types.AtxId]*types.ActivationTx
	nipsts  map[types.AtxId]*types.NIPST
	ProcCnt int
}

func NewAtxDbMock() *AtxDbMock {
	return &AtxDbMock{
		db:     make(map[types.AtxId]*types.ActivationTx),
		nipsts: make(map[types.AtxId]*types.NIPST),
	}
}

func (t *AtxDbMock) GetAtxHeader(id types.AtxId) (*types.ActivationTxHeader, error) {
	if id == *types.EmptyAtxId {
		return nil, fmt.Errorf("trying to fetch empty atx id")
	}

	if atx, ok := t.db[id]; ok {
		return atx.ActivationTxHeader, nil
	}
	return nil, fmt.Errorf("cannot find atx")
}

func (t *AtxDbMock) GetATXs(atxIds []types.AtxId) (map[types.AtxId]*types.ActivationTx, []types.AtxId) {
	return nil, nil
}

func (t *AtxDbMock) GetFullAtx(id types.AtxId) (*types.ActivationTx, error) {
	panic("implement me")
}

func (t *AtxDbMock) AddAtx(id types.AtxId, atx *types.ActivationTx) {
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
