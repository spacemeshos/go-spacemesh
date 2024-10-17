package v2alpha1

import (
	"context"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
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

		roundId = "1"
	)

	midentityStates := NewMockidentityState(gomock.NewController(t))
	db := localsql.InMemory()
	ctx := context.Background()

	configuredPoets := map[string]struct{}{
		poetAddr1: {},
		poetAddr2: {},
		poetAddr3: {},
	}

	nodeIDs := make([]types.NodeID, 0, 6)
	for range 6 {
		nodeIDs = append(nodeIDs, types.RandomNodeID())
	}

	existingIdentityStates := map[types.IdentityDescriptor]activation.IdentityState{
		newIdMock(nodeIDs[0], nodeIDs[0].String()): activation.IdentityStateWaitForATXSyncing,
		newIdMock(nodeIDs[1], nodeIDs[1].String()): activation.IdentityStateWaitForPoetRoundStart,
		newIdMock(nodeIDs[2], nodeIDs[2].String()): activation.IdentityStateWaitForPoetRoundEnd,
		newIdMock(nodeIDs[3], nodeIDs[3].String()): activation.IdentityStateWaitForPoetRoundEnd,
		newIdMock(nodeIDs[4], nodeIDs[4].String()): activation.IdentityStateFetchingProofs,
		newIdMock(nodeIDs[5], nodeIDs[5].String()): activation.IdentityStatePostProving,
	}
	midentityStates.EXPECT().IdentityStates().Return(existingIdentityStates)

	challengeHash := types.RandomHash()

	// set up db registration state:
	// 1. successful registrations:
	for _, poet := range []string{poetAddr1, poetAddr2} {
		err := nipost.AddPoetRegistration(db, nipost.PoETRegistration{
			NodeId:        nodeIDs[2],
			ChallengeHash: challengeHash,
			Address:       poet,
			RoundID:       roundId,
			RoundEnd:      time.Now().Add(1 * time.Second),
		})
		require.NoError(t, err)
	}

	// 2. successful + residual registrations:
	for _, poet := range []string{poetAddr3, poetAddr4} {
		err := nipost.AddPoetRegistration(db, nipost.PoETRegistration{
			NodeId:        nodeIDs[3],
			ChallengeHash: challengeHash,
			Address:       poet,
			RoundID:       roundId,
			RoundEnd:      time.Now().Add(1 * time.Second),
		})
		require.NoError(t, err)
	}

	// 3. registrations, which will not be returned because of
	// wrong node state
	for _, nodeId := range []types.NodeID{nodeIDs[1], nodeIDs[4], nodeIDs[5]} {
		err := nipost.AddPoetRegistration(db, nipost.PoETRegistration{
			NodeId:        nodeId,
			ChallengeHash: challengeHash,
			Address:       poetAddr4,
			RoundID:       roundId,
			RoundEnd:      time.Now().Add(3 * time.Second),
		})
		require.NoError(t, err)
	}

	// 4. failed registration:
	err := nipost.AddPoetRegistration(db, nipost.PoETRegistration{
		NodeId:        nodeIDs[2],
		ChallengeHash: challengeHash,
		Address:       poetAddr3,
	})
	require.NoError(t, err)

	svc := NewSmeshingIdentitiesService(db, configuredPoets, midentityStates)

	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	expectedRespData := map[string]struct {
		smesherId []byte
		status    pb.IdentityStatus
		poetInfos map[string]*pb.PoetServicesResponse_Identity_PoetInfo
	}{
		nodeIDs[0].String(): {
			smesherId: nodeIDs[0].Bytes(),
			status:    pb.IdentityStatus_IS_SYNCING,
		},
		nodeIDs[1].String(): {
			smesherId: nodeIDs[1].Bytes(),
			status:    pb.IdentityStatus_WAIT_FOR_POET_ROUND_START,
		},
		nodeIDs[2].String(): {
			smesherId: nodeIDs[2].Bytes(),
			status:    pb.IdentityStatus_WAIT_FOR_POET_ROUND_END,
			poetInfos: map[string]*pb.PoetServicesResponse_Identity_PoetInfo{
				poetAddr1: {
					Url:                poetAddr1,
					RegistrationStatus: pb.RegistrationStatus_SUCCESS_REG,
				},
				poetAddr2: {
					Url:                poetAddr2,
					RegistrationStatus: pb.RegistrationStatus_SUCCESS_REG,
				},
				poetAddr3: {
					Url:                poetAddr3,
					RegistrationStatus: pb.RegistrationStatus_FAILED_REG,
				},
			},
		},
		nodeIDs[3].String(): {
			smesherId: nodeIDs[3].Bytes(),
			status:    pb.IdentityStatus_WAIT_FOR_POET_ROUND_END,
			poetInfos: map[string]*pb.PoetServicesResponse_Identity_PoetInfo{
				poetAddr1: {
					Url:                poetAddr1,
					RegistrationStatus: pb.RegistrationStatus_NO_REG,
				},
				poetAddr2: {
					Url:                poetAddr2,
					RegistrationStatus: pb.RegistrationStatus_NO_REG,
				},
				poetAddr3: {
					Url:                poetAddr3,
					RegistrationStatus: pb.RegistrationStatus_SUCCESS_REG,
				},
				poetAddr4: {
					Url:                poetAddr4,
					RegistrationStatus: pb.RegistrationStatus_RESIDUAL_REG,
					Warning:            poetsMismatchWarning,
				},
			},
		},
		nodeIDs[4].String(): {
			smesherId: nodeIDs[4].Bytes(),
			status:    pb.IdentityStatus_FETCHING_PROOFS,
		},
		nodeIDs[5].String(): {
			smesherId: nodeIDs[5].Bytes(),
			status:    pb.IdentityStatus_POST_PROVING,
		},
	}

	conn := dialGrpc(t, cfg)
	client := pb.NewSmeshingIdentitiesServiceClient(conn)

	resp, err := client.PoetServices(ctx, &pb.PoetServicesRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Identities, 6)

	for _, identityResp := range resp.Identities {
		expectedResp, ok := expectedRespData[types.BytesToNodeID(identityResp.SmesherId).String()]
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
