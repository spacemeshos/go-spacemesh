package v2alpha1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"
	"time"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type malInfo struct {
	ID    types.NodeID
	Proof []byte

	Properties map[string]string
}

func TestMalfeasanceService_List(t *testing.T) {
	setup := func(t *testing.T) (spacemeshv2alpha1.MalfeasanceServiceClient, []malInfo) {
		db := statesql.InMemoryTest(t)
		ctrl := gomock.NewController(t)
		info := NewMockmalfeasanceInfo(ctrl)

		proofs := make([]malInfo, 90)
		for i := range proofs {
			proofs[i] = malInfo{ID: types.RandomNodeID(), Proof: types.RandomBytes(100)}
			proofs[i].Properties = map[string]string{
				"domain":                "0",
				"type":                  strconv.FormatUint(uint64(i%4+1), 10),
				fmt.Sprintf("key%d", i): fmt.Sprintf("value%d", i),
			}
			info.EXPECT().Info(proofs[i].Proof).Return(proofs[i].Properties, nil).AnyTimes()

			require.NoError(t, identities.SetMalicious(db, proofs[i].ID, proofs[i].Proof, time.Now()))
		}

		svc := NewMalfeasanceService(db, info)
		cfg, cleanup := launchServer(t, svc)
		t.Cleanup(cleanup)

		conn := dialGrpc(t, cfg)
		return spacemeshv2alpha1.NewMalfeasanceServiceClient(conn), proofs
	}

	t.Run("limit set too high", func(t *testing.T) {
		client, _ := setup(t)
		_, err := client.List(context.Background(), &spacemeshv2alpha1.MalfeasanceRequest{Limit: 200})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit is capped at 100", s.Message())
	})

	t.Run("no limit set", func(t *testing.T) {
		client, _ := setup(t)
		_, err := client.List(context.Background(), &spacemeshv2alpha1.MalfeasanceRequest{})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit must be set to <= 100", s.Message())
	})

	t.Run("limit and offset", func(t *testing.T) {
		client, _ := setup(t)
		list, err := client.List(context.Background(), &spacemeshv2alpha1.MalfeasanceRequest{
			Limit:  25,
			Offset: 50,
		})
		require.NoError(t, err)
		require.Len(t, list.Proofs, 25)
	})

	t.Run("all", func(t *testing.T) {
		client, _ := setup(t)
		list, err := client.List(context.Background(), &spacemeshv2alpha1.MalfeasanceRequest{Limit: 100})
		require.NoError(t, err)
		require.Len(t, list.Proofs, 90)
	})

	t.Run("smesherId", func(t *testing.T) {
		client, proofs := setup(t)
		list, err := client.List(context.Background(), &spacemeshv2alpha1.MalfeasanceRequest{
			Limit:     1,
			SmesherId: [][]byte{proofs[1].ID.Bytes()},
		})
		require.NoError(t, err)
		require.Equal(t, proofs[1].ID.Bytes(), list.GetProofs()[0].GetSmesher())
	})
}

func TestMalfeasanceStreamService_Stream(t *testing.T) {
	setup := func(
		t *testing.T,
		db sql.Executor,
		info *MockmalfeasanceInfo,
	) spacemeshv2alpha1.MalfeasanceStreamServiceClient {
		proofs := make([]malInfo, 90)
		for i := range proofs {
			proofs[i] = malInfo{ID: types.RandomNodeID(), Proof: types.RandomBytes(100)}
			proofs[i].Properties = map[string]string{
				"domain":                "0",
				"type":                  strconv.FormatUint(uint64(i%4+1), 10),
				fmt.Sprintf("key%d", i): fmt.Sprintf("value%d", i),
			}
			info.EXPECT().Info(proofs[i].Proof).Return(proofs[i].Properties, nil).AnyTimes()

			require.NoError(t, identities.SetMalicious(db, proofs[i].ID, proofs[i].Proof, time.Now()))
		}

		svc := NewMalfeasanceStreamService(db, info)
		cfg, cleanup := launchServer(t, svc)
		t.Cleanup(cleanup)

		conn := dialGrpc(t, cfg)
		return spacemeshv2alpha1.NewMalfeasanceStreamServiceClient(conn)
	}

	t.Run("all", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		ctrl := gomock.NewController(t)
		info := NewMockmalfeasanceInfo(ctrl)
		client := setup(t, statesql.InMemoryTest(t), info)

		stream, err := client.Stream(context.Background(), &spacemeshv2alpha1.MalfeasanceStreamRequest{})
		require.NoError(t, err)

		var i int
		for {
			_, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			i++
		}
		require.Equal(t, 90, i)
	})

	t.Run("watch", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		db := statesql.InMemoryTest(t)
		ctrl := gomock.NewController(t)
		info := NewMockmalfeasanceInfo(ctrl)
		client := setup(t, db, info)

		const (
			start = 100
			n     = 10
		)

		var streamed []*events.EventMalfeasance
		for i := 0; i < n; i++ {
			smesher := types.RandomNodeID()
			streamed = append(streamed, &events.EventMalfeasance{
				Smesher: smesher,
				Proof: &wire.MalfeasanceProof{
					Proof: wire.Proof{
						Type: wire.MultipleATXs,
						Data: &wire.AtxProof{
							Messages: [2]wire.AtxProofMsg{
								{
									SmesherID: smesher,
								},
								{
									SmesherID: smesher,
								},
							},
						},
					},
				},
			})
			properties := map[string]string{
				"domain":                "0",
				"type":                  strconv.FormatUint(uint64(i%4+1), 10),
				fmt.Sprintf("key%d", i): fmt.Sprintf("value%d", i),
			}
			info.EXPECT().Info(codec.MustEncode(streamed[i].Proof)).Return(properties, nil).AnyTimes()
		}

		request := &spacemeshv2alpha1.MalfeasanceStreamRequest{
			SmesherId: [][]byte{streamed[3].Smesher.Bytes()},
			Watch:     true,
		}
		stream, err := client.Stream(context.Background(), request)
		require.NoError(t, err)
		_, err = stream.Header()
		require.NoError(t, err)

		var expect []types.NodeID
		for _, rst := range streamed {
			events.ReportMalfeasance(rst.Smesher, rst.Proof)
			matcher := malfeasanceMatcher{request}
			if matcher.match(rst) {
				expect = append(expect, rst.Smesher)
			}
		}

		for _, rst := range expect {
			received, err := stream.Recv()
			require.NoError(t, err)
			require.Equal(t, rst.Bytes(), received.Smesher)
		}
	})
}
