package activation

import (
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type fetchMock struct {
	atxCalledMu sync.Mutex
	atxCalled   map[types.ATXID]int
}

func newFetchMock() *fetchMock {
	return &fetchMock{
		atxCalled: make(map[types.ATXID]int),
	}
}

func (*fetchMock) FetchBlock(types.BlockID) error {
	return nil
}

func (*fetchMock) ListenToGossip() bool {
	return true
}

func (*fetchMock) IsSynced() bool {
	return true
}

func (f *fetchMock) FetchAtx(ID types.ATXID) error {
	f.atxCalledMu.Lock()
	defer f.atxCalledMu.Unlock()

	f.atxCalled[ID]++

	return nil
}

func (*fetchMock) GetPoetProof(types.Hash32) error {
	return nil
}

func (*fetchMock) GetTxs([]types.TransactionID) error {
	return nil
}

func (*fetchMock) GetBlocks([]types.BlockID) error {
	return nil
}

func (*fetchMock) GetAtxs([]types.ATXID) error {
	return nil
}
