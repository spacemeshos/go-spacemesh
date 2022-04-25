package txs

import (
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/txs/mocks"
	txtypes "github.com/spacemeshos/go-spacemesh/txs/types"
)

func makeNanoTX(addr types.Address, fee uint64, received time.Time) *txtypes.NanoTX {
	return &txtypes.NanoTX{
		Tid:       types.RandomTransactionID(),
		Principal: addr,
		Fee:       fee,
		Received:  received,
	}
}

func makeMempool() (map[types.Address][]*txtypes.NanoTX, []types.TransactionID) {
	// acct:   [(fee, received) ....]
	// acct_0: [(4, 3), (4, 4), (4, 5)]
	// acct_1: [(2, 0), (2, 3)]
	// acct_2: [(4, 0), (3, 0), (2, 1), (5, 3)]
	now := time.Now()
	addr0 := types.Address{1, 2, 3}
	addr1 := types.Address{2, 3, 4}
	addr2 := types.Address{3, 4, 5}
	mempool := map[types.Address][]*txtypes.NanoTX{
		addr0: {
			makeNanoTX(addr0, 4, now.Add(time.Second*1)),
			makeNanoTX(addr0, 4, now.Add(time.Second*4)),
			makeNanoTX(addr0, 4, now.Add(time.Second*5)),
		},
		addr1: {
			makeNanoTX(addr1, 2, now),
			makeNanoTX(addr1, 2, now.Add(time.Second*3)),
		},
		addr2: {
			makeNanoTX(addr2, 4, now),
			makeNanoTX(addr2, 3, now),
			makeNanoTX(addr2, 2, now.Add(time.Second*1)),
			makeNanoTX(addr2, 5, now.Add(time.Second*3)),
		},
	}
	expected := []types.TransactionID{
		mempool[addr2][0].Tid, // (4, 0)
		mempool[addr0][0].Tid, // (4, 3)
		mempool[addr0][1].Tid, // (4, 4)
		mempool[addr0][2].Tid, // (4, 5)
		mempool[addr2][1].Tid, // (3, 0)
		mempool[addr1][0].Tid, // (2, 0)
		mempool[addr2][2].Tid, // (2, 1)
		mempool[addr2][3].Tid, // (5, 3)
		mempool[addr1][1].Tid, // (2, 3)
	}
	return mempool, expected
}

func TestNewMempoolIterator(t *testing.T) {
	mempool, _ := makeMempool()
	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockconStateCache(ctrl)
	mockCache.EXPECT().GetMempool().Return(mempool, nil)
	_, err := newMempoolIterator(logtest.New(t), mockCache, 100)
	require.NoError(t, err)

	errUnknown := errors.New("unknown")
	mockCache.EXPECT().GetMempool().Return(nil, errUnknown)
	_, err = newMempoolIterator(logtest.New(t), mockCache, 100)
	require.ErrorIs(t, err, errUnknown)
}

func TestPopAll(t *testing.T) {
	mempool, expected := makeMempool()
	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockconStateCache(ctrl)
	mockCache.EXPECT().GetMempool().Return(mempool, nil)
	numTXs := 3
	mi, err := newMempoolIterator(logtest.New(t), mockCache, numTXs)
	require.NoError(t, err)
	require.Equal(t, expected[0:numTXs], mi.PopAll())
	require.NotEmpty(t, mempool)
}

func TestPopAll_ExhaustMempool(t *testing.T) {
	mempool, expected := makeMempool()
	ctrl := gomock.NewController(t)
	mockCache := mocks.NewMockconStateCache(ctrl)
	mockCache.EXPECT().GetMempool().Return(mempool, nil)
	numTXs := 100
	mi, err := newMempoolIterator(logtest.New(t), mockCache, numTXs)
	require.NoError(t, err)
	require.Equal(t, expected, mi.PopAll())
	require.Empty(t, mempool)
}
