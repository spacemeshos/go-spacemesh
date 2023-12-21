package atxsync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/syncer/atxsync/mocks"
)

func atx(id types.ATXID) *types.VerifiedActivationTx {
	atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PublishEpoch: 1,
		},
		NumUnits: 1,
	}}
	atx.SetID(id)
	atx.SetEffectiveNumUnits(1)
	atx.SetReceived(time.Now())
	copy(atx.SmesherID[:], id[:])
	vatx, err := atx.Verify(0, 1)
	if err != nil {
		panic(err)
	}
	return vatx
}

func id(id ...byte) types.ATXID {
	return types.ATXID(types.BytesToHash(id))
}

type fetchRequest struct {
	request []types.ATXID
	result  []*types.VerifiedActivationTx
	error   error
}

func TestDownload(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		desc     string
		ctx      context.Context
		retry    time.Duration
		existing []*types.VerifiedActivationTx
		set      []types.ATXID
		fetched  []fetchRequest
		rst      error
	}{
		{
			desc:     "all existing",
			ctx:      context.Background(),
			existing: []*types.VerifiedActivationTx{atx(id(1)), atx(id(2)), atx(id(3))},
			set:      []types.ATXID{id(1), id(2), id(3)},
		},
		{
			desc:     "with multiple requests",
			ctx:      context.Background(),
			existing: []*types.VerifiedActivationTx{atx(id(1))},
			retry:    1,
			fetched: []fetchRequest{
				{
					request: []types.ATXID{id(2), id(3)},
					error:   errors.New("test"),
					result:  []*types.VerifiedActivationTx{atx(id(2))},
				},
				{request: []types.ATXID{id(3)}, result: []*types.VerifiedActivationTx{atx(id(3))}},
			},
			set: []types.ATXID{id(1), id(2), id(3)},
		},
		{
			desc:     "continue on error",
			ctx:      context.Background(),
			retry:    1,
			existing: []*types.VerifiedActivationTx{atx(id(1))},
			fetched: []fetchRequest{
				{request: []types.ATXID{id(2)}, error: errors.New("test")},
				{request: []types.ATXID{id(2)}, result: []*types.VerifiedActivationTx{atx(id(2))}},
			},
			set: []types.ATXID{id(1), id(2)},
		},
		{
			desc:     "exit on context",
			ctx:      canceled,
			retry:    1,
			existing: []*types.VerifiedActivationTx{atx(id(1))},
			fetched: []fetchRequest{
				{request: []types.ATXID{id(2)}, error: errors.New("test")},
			},
			set: []types.ATXID{id(1), id(2)},
			rst: context.Canceled,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			logger := logtest.New(t)
			db := sql.InMemory()
			ctrl := gomock.NewController(t)
			fetcher := mocks.NewMockatxFetcher(ctrl)
			for _, atx := range tc.existing {
				require.NoError(t, atxs.Add(db, atx))
			}
			for i := range tc.fetched {
				req := tc.fetched[i]
				fetcher.EXPECT().
					GetAtxs(tc.ctx, req.request).
					Times(1).
					DoAndReturn(func(_ context.Context, _ []types.ATXID) error {
						for _, atx := range req.result {
							require.NoError(t, atxs.Add(db, atx))
						}
						return req.error
					})
			}
			require.Equal(t, tc.rst, Download(tc.ctx, tc.retry, logger.Zap(), db, fetcher, tc.set))
		})
	}
}
