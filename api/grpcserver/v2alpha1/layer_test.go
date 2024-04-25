package v2alpha1

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"testing"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/stretchr/testify/require"
)

func TestLayerService_List(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	lrs := make([]layers.Layer, 100)
	for i := range lrs {
		l, err := generateLayer(db, types.LayerID(i), true)
		require.NoError(t, err)
		lrs[i] = *l
	}

	svc := NewLayerService(db)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewLayerServiceClient(conn)

	t.Run("limit set too high", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.LayerRequest{Limit: 200})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit is capped at 100", s.Message())
	})

	t.Run("no limit set", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.LayerRequest{})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit must be set to <= 100", s.Message())
	})

	t.Run("limit and offset", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.LayerRequest{
			Limit:  25,
			Offset: 50,
		})
		require.NoError(t, err)
		require.Len(t, list.Layers, 25)
	})

	t.Run("all", func(t *testing.T) {
		ls, err := client.List(ctx, &spacemeshv2alpha1.LayerRequest{
			StartLayer: 0,
			EndLayer:   100,
			Limit:      100,
		})
		require.NoError(t, err)
		require.Len(t, lrs, len(ls.Layers))
	})
}

func TestLayerStreamService_Stream(t *testing.T) {
	db := sql.InMemory()
	ctx := context.Background()

	lrs := make([]layers.Layer, 100)
	for i := range lrs {
		l, err := generateLayer(db, types.LayerID(i), true)
		require.NoError(t, err)
		lrs[i] = *l
	}

	svc := NewLayerStreamService(db)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewLayerStreamServiceClient(conn)

	t.Run("all", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		stream, err := client.Stream(ctx, &spacemeshv2alpha1.LayerStreamRequest{})
		require.NoError(t, err)

		var i int
		for {
			_, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			i++
		}
		require.Len(t, lrs, i)
	})

	t.Run("watch", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		const (
			start = 100
			n     = 10
		)

		var streamed []layers.Layer
		for i := start + 0; i < start+n; i++ {
			layer, err := generateLayer(db, types.LayerID(i), true)
			require.NoError(t, err)
			streamed = append(streamed, *layer)
		}

		for _, tc := range []struct {
			desc    string
			request *spacemeshv2alpha1.LayerStreamRequest
		}{
			{
				desc: "layers",
				request: &spacemeshv2alpha1.LayerStreamRequest{
					StartLayer: start,
					EndLayer:   start,
					Watch:      true,
				},
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {
				stream, err := client.Stream(ctx, tc.request)
				require.NoError(t, err)
				_, err = stream.Header()
				require.NoError(t, err)

				var expect []*layers.Layer
				for _, rst := range streamed {
					lu := events.LayerUpdate{
						LayerID: rst.Id,
						Status:  events.LayerStatusTypeApplied,
					}
					events.ReportLayerUpdate(events.LayerUpdate{
						LayerID: rst.Id,
						Status:  events.LayerStatusTypeApplied,
					})
					matcher := layersMatcher{tc.request, ctx}
					if matcher.match(&lu) {
						expect = append(expect, &rst)
					}
				}

				for _, rst := range expect {
					received, err := stream.Recv()
					require.NoError(t, err)
					require.Equal(t, toLayer(rst).String(), received.GetV1().String())
				}
			})
		}
	})
}

func generateLayer(db *sql.Database, id types.LayerID, processed bool) (*layers.Layer, error) {
	block := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: id,
			TxIDs:      nil,
		},
	}
	block.Initialize()

	err := blocks.Add(db, block)
	if err != nil {
		return nil, err
	}

	err = layers.SetApplied(db, id, block.ID())
	if err != nil {
		return nil, err
	}

	if processed {
		err = layers.SetProcessed(db, id)
		if err != nil {
			return nil, err
		}
	}

	meshHash := types.RandomHash()
	err = layers.SetMeshHash(db, id, meshHash)
	if err != nil {
		return nil, err
	}

	stateHash := types.RandomHash()
	err = layers.UpdateStateHash(db, id, stateHash)
	if err != nil {
		return nil, err
	}

	l := &layers.Layer{
		Id:             id,
		Processed:      processed,
		AppliedBlock:   block.ID(),
		StateHash:      stateHash,
		AggregatedHash: meshHash,
		Block:          block,
	}

	return l, nil
}
