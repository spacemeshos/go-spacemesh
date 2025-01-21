package v2alpha1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"strconv"
	"testing"
	"time"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type malInfo struct {
	ID    types.NodeID
	Proof []byte

	Properties map[string]string
}

func TestMalfeasanceService_List(t *testing.T) {
	db := statesql.InMemoryTest(t)
	ctrl := gomock.NewController(t)
	info := NewMockmalfeasanceInfo(ctrl)
	legacyInfo := NewMockmalfeasanceInfo(ctrl)

	proofs := make([]malInfo, 90)

	// first 20 are legacy proofs
	for i := range 20 {
		proofs[i] = malInfo{ID: types.RandomNodeID(), Proof: types.RandomBytes(100)}
		proofs[i].Properties = map[string]string{
			"domain":                "0",
			"type":                  strconv.FormatUint(uint64(i%4+1), 10),
			fmt.Sprintf("key%d", i): fmt.Sprintf("value%d", i),
		}
		info.EXPECT().Info(gomock.Any(), proofs[i].ID).Return(nil, sql.ErrNotFound).AnyTimes()
		legacyInfo.EXPECT().Info(gomock.Any(), proofs[i].ID).DoAndReturn(
			func(_ context.Context, id types.NodeID) (map[string]string, error) {
				return maps.Clone(proofs[i].Properties), nil
			}).AnyTimes()
		require.NoError(t, identities.SetMalicious(db, proofs[i].ID, proofs[i].Proof, time.Now()))
	}

	// next 50 are proofs for individual identities
	for i := 20; i < 70; i++ {
		proofs[i] = malInfo{ID: types.RandomNodeID(), Proof: types.RandomBytes(100)}
		proofs[i].Properties = map[string]string{
			"domain":                strconv.FormatUint(uint64(i%4+1), 10),
			"type":                  strconv.FormatUint(uint64(i%4+1), 10),
			fmt.Sprintf("key%d", i): fmt.Sprintf("value%d", i),
		}
		info.EXPECT().Info(gomock.Any(), proofs[i].ID).DoAndReturn(
			func(_ context.Context, id types.NodeID) (map[string]string, error) {
				return maps.Clone(proofs[i].Properties), nil
			}).AnyTimes()
		legacyInfo.EXPECT().Info(gomock.Any(), proofs[i].ID).Return(nil, sql.ErrNotFound).AnyTimes()
		require.NoError(t, malfeasance.AddProof(db, proofs[i].ID, nil, proofs[i].Proof, i%4+1, time.Now()))
	}

	// last 20 are proofs for a single marriage
	id, err := marriage.NewID(db)
	require.NoError(t, err)
	marriageATX := types.RandomATXID()
	for i := 70; i < 90; i++ {
		proofs[i] = malInfo{ID: types.RandomNodeID()}
		err := marriage.Add(db, marriage.Info{
			ID:            id,
			NodeID:        proofs[i].ID,
			ATX:           marriageATX,
			MarriageIndex: i % 70,
			Target:        proofs[70].ID,
			Signature:     types.RandomEdSignature(),
		})
		require.NoError(t, err)
	}
	proofs[70].Proof = types.RandomBytes(100)
	proofs[70].Properties = map[string]string{
		"domain": "1",
		"type":   "1",
		"key":    "value",
	}
	info.EXPECT().Info(gomock.Any(), proofs[70].ID).DoAndReturn(
		func(_ context.Context, id types.NodeID) (map[string]string, error) {
			return maps.Clone(proofs[70].Properties), nil
		}).AnyTimes()
	legacyInfo.EXPECT().Info(gomock.Any(), proofs[70].ID).Return(nil, sql.ErrNotFound).AnyTimes()
	require.NoError(t, malfeasance.AddProof(db, proofs[70].ID, &id, proofs[70].Proof, 1, time.Now()))
	for i := 71; i < 90; i++ {
		proofs[i] = malInfo{ID: proofs[i].ID, Proof: proofs[70].Proof}
		proofs[i].Properties = maps.Clone(proofs[70].Properties)
		proofs[i].Properties["malicious_id"] = proofs[i].ID.String()

		info.EXPECT().Info(gomock.Any(), proofs[i].ID).DoAndReturn(
			func(_ context.Context, id types.NodeID) (map[string]string, error) {
				return maps.Clone(proofs[i].Properties), nil
			}).AnyTimes()
		legacyInfo.EXPECT().Info(gomock.Any(), proofs[i].ID).Return(nil, sql.ErrNotFound).AnyTimes()

		require.NoError(t, malfeasance.SetMalicious(db, proofs[i].ID, id, time.Now()))
	}

	svc := NewMalfeasanceService(db, info, legacyInfo)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	t.Run("limit set too high", func(t *testing.T) {
		client := spacemeshv2alpha1.NewMalfeasanceServiceClient(dialGrpc(t, cfg))
		_, err := client.List(context.Background(), &spacemeshv2alpha1.MalfeasanceRequest{Limit: 200})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit is capped at 100", s.Message())
	})

	t.Run("no limit set", func(t *testing.T) {
		client := spacemeshv2alpha1.NewMalfeasanceServiceClient(dialGrpc(t, cfg))
		_, err := client.List(context.Background(), &spacemeshv2alpha1.MalfeasanceRequest{})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit must be set to <= 100", s.Message())
	})

	t.Run("limit and offset", func(t *testing.T) {
		client := spacemeshv2alpha1.NewMalfeasanceServiceClient(dialGrpc(t, cfg))
		list, err := client.List(context.Background(), &spacemeshv2alpha1.MalfeasanceRequest{
			Limit:  25,
			Offset: 50,
		})
		require.NoError(t, err)
		require.Len(t, list.Proofs, 25)
	})

	t.Run("all", func(t *testing.T) {
		client := spacemeshv2alpha1.NewMalfeasanceServiceClient(dialGrpc(t, cfg))
		list, err := client.List(context.Background(), &spacemeshv2alpha1.MalfeasanceRequest{Limit: 100})
		require.NoError(t, err)
		require.Len(t, list.Proofs, 90)
	})

	t.Run("smesherId", func(t *testing.T) {
		client := spacemeshv2alpha1.NewMalfeasanceServiceClient(dialGrpc(t, cfg))
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
		legacyInfo *MockmalfeasanceInfo,
	) spacemeshv2alpha1.MalfeasanceStreamServiceClient {
		proofs := make([]malInfo, 90)
		// first 20 are legacy proofs
		for i := range 20 {
			proofs[i] = malInfo{ID: types.RandomNodeID(), Proof: types.RandomBytes(100)}
			proofs[i].Properties = map[string]string{
				"domain":                "0",
				"type":                  strconv.FormatUint(uint64(i%4+1), 10),
				fmt.Sprintf("key%d", i): fmt.Sprintf("value%d", i),
			}
			info.EXPECT().Info(gomock.Any(), proofs[i].ID).Return(nil, sql.ErrNotFound).AnyTimes()
			legacyInfo.EXPECT().Info(gomock.Any(), proofs[i].ID).DoAndReturn(
				func(_ context.Context, id types.NodeID) (map[string]string, error) {
					return maps.Clone(proofs[i].Properties), nil
				}).AnyTimes()
			require.NoError(t, identities.SetMalicious(db, proofs[i].ID, proofs[i].Proof, time.Now()))
		}

		// next 50 are proofs for individual identities
		for i := 20; i < 70; i++ {
			proofs[i] = malInfo{ID: types.RandomNodeID(), Proof: types.RandomBytes(100)}
			proofs[i].Properties = map[string]string{
				"domain":                strconv.FormatUint(uint64(i%4+1), 10),
				"type":                  strconv.FormatUint(uint64(i%4+1), 10),
				fmt.Sprintf("key%d", i): fmt.Sprintf("value%d", i),
			}
			info.EXPECT().Info(gomock.Any(), proofs[i].ID).DoAndReturn(
				func(_ context.Context, id types.NodeID) (map[string]string, error) {
					return maps.Clone(proofs[i].Properties), nil
				}).AnyTimes()
			legacyInfo.EXPECT().Info(gomock.Any(), proofs[i].ID).Return(nil, sql.ErrNotFound).AnyTimes()
			require.NoError(t, malfeasance.AddProof(db, proofs[i].ID, nil, proofs[i].Proof, i%4+1, time.Now()))
		}

		// last 20 are proofs for a single marriage
		id, err := marriage.NewID(db)
		require.NoError(t, err)
		marriageATX := types.RandomATXID()
		for i := 70; i < 90; i++ {
			proofs[i] = malInfo{ID: types.RandomNodeID()}
			err := marriage.Add(db, marriage.Info{
				ID:            id,
				NodeID:        proofs[i].ID,
				ATX:           marriageATX,
				MarriageIndex: i % 70,
				Target:        proofs[70].ID,
				Signature:     types.RandomEdSignature(),
			})
			require.NoError(t, err)
		}
		proofs[70].Proof = types.RandomBytes(100)
		proofs[70].Properties = map[string]string{
			"domain": "1",
			"type":   "1",
			"key":    "value",
		}
		info.EXPECT().Info(gomock.Any(), proofs[70].ID).DoAndReturn(
			func(_ context.Context, id types.NodeID) (map[string]string, error) {
				return maps.Clone(proofs[70].Properties), nil
			}).AnyTimes()
		legacyInfo.EXPECT().Info(gomock.Any(), proofs[70].ID).Return(nil, sql.ErrNotFound).AnyTimes()
		require.NoError(t, malfeasance.AddProof(db, proofs[70].ID, &id, proofs[70].Proof, 1, time.Now()))
		for i := 71; i < 90; i++ {
			proofs[i] = malInfo{ID: proofs[i].ID, Proof: proofs[70].Proof}
			proofs[i].Properties = maps.Clone(proofs[70].Properties)
			proofs[i].Properties["malicious_id"] = proofs[i].ID.String()

			info.EXPECT().Info(gomock.Any(), proofs[i].ID).DoAndReturn(
				func(_ context.Context, id types.NodeID) (map[string]string, error) {
					return maps.Clone(proofs[i].Properties), nil
				}).AnyTimes()
			legacyInfo.EXPECT().Info(gomock.Any(), proofs[i].ID).Return(nil, sql.ErrNotFound).AnyTimes()

			require.NoError(t, malfeasance.SetMalicious(db, proofs[i].ID, id, time.Now()))
		}

		svc := NewMalfeasanceStreamService(db, info, legacyInfo)
		cfg, cleanup := launchServer(t, svc)
		t.Cleanup(cleanup)

		conn := dialGrpc(t, cfg)
		return spacemeshv2alpha1.NewMalfeasanceStreamServiceClient(conn)
	}

	t.Run("all", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		db := statesql.InMemoryTest(t)
		ctrl := gomock.NewController(t)
		info := NewMockmalfeasanceInfo(ctrl)
		legacyInfo := NewMockmalfeasanceInfo(ctrl)
		client := setup(t, db, info, legacyInfo)

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
		legacyInfo := NewMockmalfeasanceInfo(ctrl)
		client := setup(t, db, info, legacyInfo)

		const (
			nLegacy     = 5
			nIndividual = 5
			nMarriage   = 5
		)

		streamed := make([]*events.EventMalfeasance, 0, nLegacy+nIndividual+nMarriage)
		for i := 0; i < nLegacy; i++ {
			smesher := types.RandomNodeID()
			streamed = append(streamed, &events.EventMalfeasance{
				Smesher: smesher,
			})
			properties := map[string]string{
				"domain":                "0",
				"type":                  strconv.FormatUint(uint64(i%4+1), 10),
				fmt.Sprintf("key%d", i): fmt.Sprintf("value%d", i),
			}
			info.EXPECT().Info(gomock.Any(), streamed[i].Smesher).Return(nil, sql.ErrNotFound).AnyTimes()
			legacyInfo.EXPECT().Info(gomock.Any(), streamed[i].Smesher).DoAndReturn(
				func(_ context.Context, id types.NodeID) (map[string]string, error) {
					return maps.Clone(properties), nil
				}).AnyTimes()
		}

		for i := nLegacy; i < nLegacy+nIndividual; i++ {
			smesher := types.RandomNodeID()
			streamed = append(streamed, &events.EventMalfeasance{
				Smesher: smesher,
			})
			properties := map[string]string{
				"domain":                strconv.FormatUint(uint64(i%4+1), 10),
				"type":                  strconv.FormatUint(uint64(i%4+1), 10),
				fmt.Sprintf("key%d", i): fmt.Sprintf("value%d", i),
			}
			info.EXPECT().Info(gomock.Any(), streamed[i].Smesher).DoAndReturn(
				func(_ context.Context, id types.NodeID) (map[string]string, error) {
					return maps.Clone(properties), nil
				}).AnyTimes()
			legacyInfo.EXPECT().Info(gomock.Any(), streamed[i].Smesher).Return(nil, sql.ErrNotFound).AnyTimes()
		}

		id, err := marriage.NewID(db)
		require.NoError(t, err)
		marriageATX := types.RandomATXID()
		for i := nLegacy + nIndividual; i < nLegacy+nIndividual+nMarriage; i++ {
			smesher := types.RandomNodeID()
			streamed = append(streamed, &events.EventMalfeasance{
				Smesher: smesher,
			})
			properties := map[string]string{
				"domain": "1",
				"type":   "1",
				"key":    "value",
			}
			info.EXPECT().Info(gomock.Any(), streamed[i].Smesher).DoAndReturn(
				func(_ context.Context, id types.NodeID) (map[string]string, error) {
					return maps.Clone(properties), nil
				}).AnyTimes()
			legacyInfo.EXPECT().Info(gomock.Any(), streamed[i].Smesher).Return(nil, sql.ErrNotFound).AnyTimes()

			err := marriage.Add(db, marriage.Info{
				ID:            id,
				NodeID:        streamed[i].Smesher,
				ATX:           marriageATX,
				MarriageIndex: i%nLegacy + nIndividual,
				Target:        streamed[nLegacy+nIndividual].Smesher,
				Signature:     types.RandomEdSignature(),
			})
			require.NoError(t, err)
		}

		request := &spacemeshv2alpha1.MalfeasanceStreamRequest{
			SmesherId: [][]byte{streamed[3].Smesher.Bytes(), streamed[7].Smesher.Bytes(), streamed[12].Smesher.Bytes()},
			Watch:     true,
		}
		stream, err := client.Stream(context.Background(), request)
		require.NoError(t, err)
		_, err = stream.Header()
		require.NoError(t, err)

		expect := make([]types.NodeID, 0, len(request.SmesherId))
		for _, rst := range streamed {
			events.ReportMalfeasance(rst.Smesher)
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
