package v2alpha1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/activation"
)

const SmeshingIdentities = "smeshing_identities_v2alpha1"

type SmeshingIdentitiesService struct {
	states identityState
}

func NewSmeshingIdentitiesService(
	states identityState,
) *SmeshingIdentitiesService {
	return &SmeshingIdentitiesService{
		states: states,
	}
}

var statusMap = map[activation.IdentityState]pb.IdentityState{
	activation.IdentityStateNotSet:                           pb.IdentityState_UNSPECIFIED,
	activation.IdentityStateWaitForATXSynced:                 pb.IdentityState_WAIT_FOR_ATX_SYNCED,
	activation.IdentityStateRetrying:                         pb.IdentityState_RETRYING,
	activation.IdentityStateWaitingForPoetRegistrationWindow: pb.IdentityState_WAITING_FOR_POET_REGISTRATION_WINDOW,
	activation.IdentityStatePoetChallengeReady:               pb.IdentityState_POET_CHALLENGE_READY,
	activation.IdentityStatePoetRegistered:                   pb.IdentityState_POET_REGISTERED,
	activation.IdentityStateWaitForPoetRoundEnd:              pb.IdentityState_WAIT_FOR_POET_ROUND_END,
	activation.IdentityStatePoetProofReceived:                pb.IdentityState_POET_PROOF_RECEIVED,
	activation.IdentityStateGeneratingPostProof:              pb.IdentityState_GENERATING_POST_PROOF,
	activation.IdentityStatePostProofReady:                   pb.IdentityState_POST_PROOF_READY,
	activation.IdentityStateATXReady:                         pb.IdentityState_ATX_READY,
	activation.IdentityStateATXBroadcasted:                   pb.IdentityState_ATX_BROADCASTED,
}

func (s *SmeshingIdentitiesService) RegisterService(server *grpc.Server) {
	pb.RegisterSmeshingIdentitiesServiceServer(server, s)
}

func (s *SmeshingIdentitiesService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterSmeshingIdentitiesServiceHandlerServer(context.Background(), mux, s)
}

// String returns the name of this service.
func (s *SmeshingIdentitiesService) String() string {
	return "SmeshingIdentitiesService"
}

func (s *SmeshingIdentitiesService) States(
	ctx context.Context,
	_ *pb.IdentityStatesRequest,
) (*pb.IdentityStatesResponse, error) {
	pbIdentities := make(map[string]*pb.Identity)

	for nodeId, history := range s.states.All() {
		pbIdentities[nodeId.String()] = &pb.Identity{
			History: []*pb.IdentityStateInfo{},
		}

		for i := len(history) - 1; i >= 0; i-- {
			info := history[i]
			ts := timestamppb.New(info.Time)
			identityStateInfo := &pb.IdentityStateInfo{
				State:   statusMap[info.State],
				Time:    ts,
				Message: info.Message,
			}
			if info.PublishEpoch != nil {
				epoch := info.PublishEpoch.Uint32()
				identityStateInfo.PublishEpoch = &epoch
			}
			pbIdentities[nodeId.String()].History = append(pbIdentities[nodeId.String()].History, identityStateInfo)
		}
	}

	return &pb.IdentityStatesResponse{Identities: pbIdentities}, nil
}
