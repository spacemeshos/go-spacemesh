package activation

import (
	"context"
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

func (*fetchMock) FetchBlock(context.Context, types.BlockID) error {
	return nil
}

func (*fetchMock) ListenToGossip() bool {
	return true
}

func (*fetchMock) IsSynced(context.Context) bool {
	return true
}

func (f *fetchMock) FetchAtx(ctx context.Context, ID types.ATXID) error {
	f.atxCalledMu.Lock()
	defer f.atxCalledMu.Unlock()

	f.atxCalled[ID]++

	return nil
}

func (*fetchMock) GetPoetProof(context.Context, types.Hash32) error {
	return nil
}

func (*fetchMock) GetTxs(context.Context, []types.TransactionID) error {
	return nil
}

func (*fetchMock) GetBlocks(context.Context, []types.BlockID) error {
	return nil
}

func (*fetchMock) GetAtxs(context.Context, []types.ATXID) error {
	return nil
}
