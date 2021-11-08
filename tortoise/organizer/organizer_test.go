package organizer_test

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoise/organizer"
)

func TestOrder(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		send   []types.LayerID
		expect []types.LayerID
		last   types.LayerID
		window int
	}{
		{
			desc:   "Inorder",
			send:   []types.LayerID{types.NewLayerID(1), types.NewLayerID(2), types.NewLayerID(3)},
			expect: []types.LayerID{types.NewLayerID(1), types.NewLayerID(2), types.NewLayerID(3)},
			window: 3,
		},
		{
			desc:   "InorderWithLast",
			send:   []types.LayerID{types.NewLayerID(11), types.NewLayerID(12), types.NewLayerID(13)},
			expect: []types.LayerID{types.NewLayerID(11), types.NewLayerID(12), types.NewLayerID(13)},
			last:   types.NewLayerID(10),
			window: 3,
		},
		{
			desc:   "OutOfOrder",
			send:   []types.LayerID{types.NewLayerID(1), types.NewLayerID(3), types.NewLayerID(2)},
			expect: []types.LayerID{types.NewLayerID(1), types.NewLayerID(2), types.NewLayerID(3)},
			window: 3,
		},
		{
			desc: "OutOfOrderMany",
			send: []types.LayerID{
				types.NewLayerID(1), types.NewLayerID(3), types.NewLayerID(5),
				types.NewLayerID(2), types.NewLayerID(4),
			},
			expect: []types.LayerID{
				types.NewLayerID(1), types.NewLayerID(2), types.NewLayerID(3),
				types.NewLayerID(4), types.NewLayerID(5),
			},
			window: 5,
		},
		{
			desc:   "OldLayer",
			send:   []types.LayerID{types.NewLayerID(1), types.NewLayerID(2), types.NewLayerID(1)},
			expect: []types.LayerID{types.NewLayerID(1), types.NewLayerID(2)},
			window: 3,
		},
		{
			desc: "InorderOverflow",
			send: []types.LayerID{
				types.NewLayerID(1), types.NewLayerID(2), types.NewLayerID(3),
				types.NewLayerID(4), types.NewLayerID(5), types.NewLayerID(6),
			},
			expect: []types.LayerID{
				types.NewLayerID(1), types.NewLayerID(2), types.NewLayerID(3),
				types.NewLayerID(4), types.NewLayerID(5), types.NewLayerID(6),
			},
			window: 3,
		},
		{
			desc: "WindowOverflow",
			send: []types.LayerID{
				types.NewLayerID(1), types.NewLayerID(2),
				types.NewLayerID(4), types.NewLayerID(5), types.NewLayerID(6),
			},
			expect: []types.LayerID{
				types.NewLayerID(1), types.NewLayerID(2),
				types.NewLayerID(4), types.NewLayerID(5), types.NewLayerID(6),
			},
			window: 3,
		},
		{
			desc: "OverflowFromStart",
			send: []types.LayerID{
				types.NewLayerID(4), types.NewLayerID(5), types.NewLayerID(6),
			},
			expect: []types.LayerID{
				types.NewLayerID(4), types.NewLayerID(5), types.NewLayerID(6),
			},
			window: 3,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			ctx := context.Background()
			ctrl := gomock.NewController(t)
			tortoise := mocks.NewMockTortoise(ctrl)
			var rst []types.LayerID
			tortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, lid types.LayerID) (types.LayerID, types.LayerID, bool) {
					rst = append(rst, lid)
					return lid.Sub(1), lid, false
				}).AnyTimes()

			org := organizer.New(tortoise,
				organizer.WithWindowSize(tc.window),
				organizer.WithVerifiedLayer(tc.last),
				organizer.WithLogger(logtest.New(t)),
			)
			for _, lid := range tc.send {
				org.HandleIncomingLayer(ctx, lid)
			}
			require.Equal(t, tc.expect, rst)
		})
	}
}
