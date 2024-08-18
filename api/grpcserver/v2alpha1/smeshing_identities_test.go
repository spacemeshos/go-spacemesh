package v2alpha1

import (
	"context"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"testing"
	"time"
)

type idMock struct {
	id   types.NodeID
	name string
}

func newIdMock(id types.NodeID, name string) idMock {
	return idMock{
		id:   id,
		name: name,
	}
}

func (i idMock) NodeID() types.NodeID {
	return i.id
}

func (i idMock) Name() string {
	return i.name
}

func TestSmeshingIdentitiesServices(t *testing.T) {
	const (
		poetAddr1 = "localhost:8081"
		poetAddr2 = "localhost:8082"
		poetAddr3 = "localhost:8083"
		poetAddr4 = "localhost:8084"
	)

	midentityStates := NewMockidentityState(gomock.NewController(t))
	db := localsql.InMemory()
	ctx := context.Background()

	configuredPoets := map[string]struct {
	}{
		poetAddr1: {},
		poetAddr2: {},
		poetAddr3: {},
	}

	nodeId1 := types.RandomNodeID()
	nodeId2 := types.RandomNodeID()
	nodeId3 := types.RandomNodeID()
	nodeId4 := types.RandomNodeID()
	nodeId5 := types.RandomNodeID()
	nodeId6 := types.RandomNodeID()

	nodeIds := map[types.NodeID]struct{}{
		nodeId1: {},
		nodeId2: {},
		nodeId3: {},
		nodeId4: {},
		nodeId5: {},
		nodeId6: {},
	}

	existingIdentityStates := map[types.IdentityDescriptor]types.IdentityState{
		newIdMock(nodeId1, nodeId1.String()): types.WaitForATXSyncing,
		newIdMock(nodeId2, nodeId2.String()): types.WaitForPoetRoundStart,
		newIdMock(nodeId3, nodeId3.String()): types.WaitForPoetRoundEnd,
		newIdMock(nodeId4, nodeId4.String()): types.WaitForPoetRoundEnd,
		newIdMock(nodeId5, nodeId5.String()): types.FetchingProofs,
		newIdMock(nodeId6, nodeId5.String()): types.PostProving,
	}
	midentityStates.EXPECT().IdentityStates().Return(existingIdentityStates)

	challengeHash := types.RandomHash()

	// set up db registration state
	err := nipost.AddPoetRegistration(db, nipost.PoETRegistration{
		NodeId:        nodeId3,
		ChallengeHash: challengeHash,
		Address:       poetAddr1,
		RoundID:       "1",
		RoundEnd:      time.Now().Add(1 * time.Second),
	})
	require.NoError(t, err)

	err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
		NodeId:        nodeId3,
		ChallengeHash: challengeHash,
		Address:       poetAddr2,
		RoundID:       "1",
		RoundEnd:      time.Now().Add(1 * time.Second),
	})
	require.NoError(t, err)

	err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
		NodeId:        nodeId3,
		ChallengeHash: challengeHash,
		Address:       poetAddr3,
		RoundEnd:      time.Now().Add(3 * time.Second),
	})
	require.NoError(t, err)

	err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
		NodeId:        nodeId4,
		ChallengeHash: challengeHash,
		RoundID:       "1",
		Address:       poetAddr3,
		RoundEnd:      time.Now().Add(3 * time.Second),
	})
	require.NoError(t, err)

	err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
		NodeId:        nodeId4,
		ChallengeHash: challengeHash,
		RoundID:       "1",
		Address:       poetAddr4,
		RoundEnd:      time.Now().Add(3 * time.Second),
	})
	require.NoError(t, err)

	err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
		NodeId:        nodeId2,
		ChallengeHash: challengeHash,
		Address:       poetAddr4,
		RoundEnd:      time.Now().Add(3 * time.Second),
	})
	require.NoError(t, err)

	err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
		NodeId:        nodeId5,
		ChallengeHash: challengeHash,
		Address:       poetAddr3,
		RoundEnd:      time.Now().Add(3 * time.Second),
	})
	require.NoError(t, err)

	err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
		NodeId:        nodeId6,
		ChallengeHash: challengeHash,
		Address:       poetAddr2,
		RoundEnd:      time.Now().Add(3 * time.Second),
	})
	require.NoError(t, err)

	svc := NewSmeshingIdentitiesService(db, configuredPoets, midentityStates, nodeIds)

	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	expectedRespData := map[string]struct {
		smesherIdHex string
		status       pb.IdentityStatus
		poetInfos    map[string]*pb.PoetServicesResponse_Identity_PoetInfo
	}{
		nodeId1.String(): {
			smesherIdHex: nodeId1.String(),
			status:       pb.IdentityStatus_STATUS_IS_SYNCING,
		},
		nodeId2.String(): {
			smesherIdHex: nodeId2.String(),
			status:       pb.IdentityStatus_STATUS_WAIT_FOR_POET_ROUND_START,
		},
		nodeId3.String(): {
			smesherIdHex: nodeId3.String(),
			status:       pb.IdentityStatus_STATUS_WAIT_FOR_POET_ROUND_END,
			poetInfos: map[string]*pb.PoetServicesResponse_Identity_PoetInfo{
				poetAddr1: {
					Url:                poetAddr1,
					RegistrationStatus: pb.RegistrationStatus_STATUS_SUCCESS_REG,
				},
				poetAddr2: {
					Url:                poetAddr2,
					RegistrationStatus: pb.RegistrationStatus_STATUS_SUCCESS_REG,
				},
				poetAddr3: {
					Url:                poetAddr3,
					RegistrationStatus: pb.RegistrationStatus_STATUS_FAILED_REG,
				},
			},
		},
		nodeId4.String(): {
			smesherIdHex: nodeId4.String(),
			status:       pb.IdentityStatus_STATUS_WAIT_FOR_POET_ROUND_END,
			poetInfos: map[string]*pb.PoetServicesResponse_Identity_PoetInfo{
				poetAddr1: {
					Url:                poetAddr1,
					RegistrationStatus: pb.RegistrationStatus_STATUS_NO_REG,
				},
				poetAddr2: {
					Url:                poetAddr2,
					RegistrationStatus: pb.RegistrationStatus_STATUS_NO_REG,
				},
				poetAddr3: {
					Url:                poetAddr3,
					RegistrationStatus: pb.RegistrationStatus_STATUS_SUCCESS_REG,
				},
				poetAddr4: {
					Url:                poetAddr4,
					RegistrationStatus: pb.RegistrationStatus_STATUS_RESIDUAL_REG,
					Warning:            poetsMismatchWarning,
				},
			},
		},
		nodeId5.String(): {
			smesherIdHex: nodeId5.String(),
			status:       pb.IdentityStatus_STATUS_FETCHING_PROOFS,
		},
		nodeId6.String(): {
			smesherIdHex: nodeId6.String(),
			status:       pb.IdentityStatus_STATUS_POST_PROVING,
		},
	}

	conn := dialGrpc(t, cfg)
	client := pb.NewSmeshingIdentitiesServiceClient(conn)

	resp, err := client.PoetServices(ctx, &pb.PoetServicesRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Identities, 6)

	for _, identityResp := range resp.Identities {
		expectedResp, ok := expectedRespData[identityResp.SmesherIdHex]
		require.True(t, ok)
		require.Equal(t, expectedResp.status, identityResp.Status)
		require.Equal(t, len(expectedResp.poetInfos), len(identityResp.PoetInfos))

		for _, poetInfo := range identityResp.PoetInfos {
			expectedInfo, ok := expectedResp.poetInfos[poetInfo.Url]
			require.True(t, ok)
			require.Equal(t, expectedInfo.RegistrationStatus, poetInfo.RegistrationStatus)
			require.Equal(t, expectedInfo.Warning, poetInfo.Warning)
		}
	}
}
