package tortoisebeacon

import "github.com/spacemeshos/go-spacemesh/common/types"

type mockATXGetter struct {
	atxList []types.ATXID
}

func newMockATXGetter(atxList []types.ATXID) *mockATXGetter {
	return &mockATXGetter{atxList: atxList}
}

func (m mockATXGetter) GetEpochAtxs(epochID types.EpochID) (atxs []types.ATXID) {
	return m.atxList
}

type MockTotalWeightGetter struct{}

func (m MockTotalWeightGetter) GetTotalWeight() int {
	return 0
}
