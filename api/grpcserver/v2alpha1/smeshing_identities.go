package v2alpha1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"golang.org/x/exp/maps"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
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

var statusMap = map[activation.IdentityState]pb.IdentityStatus{
	activation.IdentityStateNotSet:                           pb.IdentityStatus_UNSPECIFIED,
	activation.IdentityStateWaitForATXSyncing:                pb.IdentityStatus_WAIT_FOR_ATX_SYNCING,
	activation.IdentityStateWaitingForPoetRegistrationWindow: pb.IdentityStatus_WAITING_FOR_POET_REGISTRATION_WINDOW,
	activation.IdentityStatePoetChallengeReady:               pb.IdentityStatus_POET_CHALLENGE_READY,
	activation.IdentityStatePoetRegistered:                   pb.IdentityStatus_POET_REGISTERED,
	activation.IdentityStatePoetRegistrationFailed:           pb.IdentityStatus_POET_REGISTRATION_FAILED,
	activation.IdentityStateWaitForPoetRoundEnd:              pb.IdentityStatus_WAIT_FOR_POET_ROUND_END,
	activation.IdentityStatePoetProofReceived:                pb.IdentityStatus_POET_PROOF_RECEIVED,
	activation.IdentityStatePoetProofFailed:                  pb.IdentityStatus_POET_PROOF_FAILED,
	activation.IdentityStateGeneratingPostProof:              pb.IdentityStatus_GENERATING_POST_PROOF,
	activation.IdentityStatePostProofReady:                   pb.IdentityStatus_POST_PROOF_READY,
	activation.IdentityStatePostProofFailed:                  pb.IdentityStatus_POST_PROOF_FAILED,
	activation.IdentityStateATXExpired:                       pb.IdentityStatus_ATX_EXPIRED,
	activation.IdentityStateATXReady:                         pb.IdentityStatus_ATX_READY,
	activation.IdentityStateATXBroadcasted:                   pb.IdentityStatus_ATX_BROADCASTED,
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
	pbIdentities := make(map[types.NodeID]*pb.Identity)

	for desc, state := range s.states.All() {
		pbIdentities[desc] = &pb.Identity{
			SmesherId: desc.Bytes(),
			Epochs:    []*pb.IdentityStateEpoch{},
			States:    []*pb.IdentityState{},
		}

		for epoch, info := range state.EpochStates {
			pbEpoch := &pb.IdentityStateEpoch{
				Epoch:  epoch.Uint32(),
				States: []*pb.IdentityState{},
			}

			for status, statusInfo := range info.States {
				ts := timestamppb.New(statusInfo.Time)
				pbEpoch.States = append(pbEpoch.States, &pb.IdentityState{
					State:   statusMap[status],
					Time:    ts,
					Message: statusInfo.Message,
				})
			}

			pbIdentities[desc].Epochs = append(pbIdentities[desc].Epochs, pbEpoch)
		}

		for status, statusInfo := range state.States {
			ts := timestamppb.New(statusInfo.Time)
			pbIdentities[desc].States = append(pbIdentities[desc].States, &pb.IdentityState{
				State:   statusMap[status],
				Time:    ts,
				Message: statusInfo.Message,
			})
		}
	}

	return &pb.IdentityStatesResponse{Identities: maps.Values(pbIdentities)}, nil
}
