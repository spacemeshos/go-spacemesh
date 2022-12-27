package txs

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func makeNanoTX(addr types.Address, fee uint64, received time.Time) *NanoTX {
	return &NanoTX{
		ID: types.RandomTransactionID(),
		TxHeader: types.TxHeader{
			Principal: addr,
			GasPrice:  fee,
			MaxGas:    1,
		},
		Received: received,
	}
}

func makeMempool() (map[types.Address][]*NanoTX, []*NanoTX) {
	// acct:   [(fee, received) ....]
	// acct_0: [(4, 3), (4, 4), (4, 5)]
	// acct_1: [(2, 0), (2, 3)]
	// acct_2: [(4, 0), (3, 0), (2, 1), (5, 3)]
	now := time.Now()
	addr0 := types.Address{1, 2, 3}
	addr1 := types.Address{2, 3, 4}
	addr2 := types.Address{3, 4, 5}
	mempool := map[types.Address][]*NanoTX{
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
	expected := []*NanoTX{
		mempool[addr2][0], // (4, 0)
		mempool[addr0][0], // (4, 3)
		mempool[addr0][1], // (4, 4)
		mempool[addr0][2], // (4, 5)
		mempool[addr2][1], // (3, 0)
		mempool[addr1][0], // (2, 0)
		mempool[addr2][2], // (2, 1)
		mempool[addr2][3], // (5, 3)
		mempool[addr1][1], // (2, 3)
	}
	return mempool, expected
}

func testPopAll(t *testing.T, mi *mempoolIterator, expected []*NanoTX) {
	got, byAddrAndNonce := mi.PopAll()
	require.Equal(t, expected, got)
	for _, ntx := range got {
		ntxs, ok := byAddrAndNonce[ntx.Principal]
		require.True(t, ok)
		if len(ntxs) > 1 {
			byAddrAndNonce[ntx.Principal] = byAddrAndNonce[ntx.Principal][1:]
		} else {
			delete(byAddrAndNonce, ntx.Principal)
		}
	}
	require.Empty(t, byAddrAndNonce)
}

func TestPopAll(t *testing.T) {
	mempool, expected := makeMempool()
	ctrl := gomock.NewController(t)
	mockCache := NewMockconStateCache(ctrl)
	mockCache.EXPECT().GetMempool(gomock.Any()).Return(mempool)
	gasLimit := uint64(3)
	mi := newMempoolIterator(logtest.New(t), mockCache, gasLimit)
	testPopAll(t, mi, expected[:gasLimit])
	require.NotEmpty(t, mempool)
}

func TestPopAll_SkipSomeGasTooHigh(t *testing.T) {
	mempool, orderedByFee := makeMempool()
	ctrl := gomock.NewController(t)
	mockCache := NewMockconStateCache(ctrl)
	mockCache.EXPECT().GetMempool(gomock.Any()).Return(mempool)
	gasLimit := uint64(3)
	// make the 2nd one too expensive to pick, therefore invalidated all txs from addr0
	orderedByFee[1].MaxGas = 10
	expected := []*NanoTX{orderedByFee[0], orderedByFee[4], orderedByFee[5]}
	mi := newMempoolIterator(logtest.New(t), mockCache, gasLimit)
	testPopAll(t, mi, expected)
	require.NotEmpty(t, mempool)
}

func TestPopAll_ExhaustMempool(t *testing.T) {
	mempool, expected := makeMempool()
	ctrl := gomock.NewController(t)
	mockCache := NewMockconStateCache(ctrl)
	mockCache.EXPECT().GetMempool(gomock.Any()).Return(mempool)
	gasLimit := uint64(100)
	mi := newMempoolIterator(logtest.New(t), mockCache, gasLimit)
	testPopAll(t, mi, expected)
	require.Empty(t, mempool)
}
