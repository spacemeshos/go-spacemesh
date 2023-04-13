package grpcserver

import (
	"context"
	"fmt"

	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

// DebugService exposes global state data, output from the STF.
type DebugService struct {
	conState api.ConservativeState
	identity api.NetworkIdentity
}

// RegisterService registers this service with a grpc server instance.
func (d DebugService) RegisterService(server *Server) {
	pb.RegisterDebugServiceServer(server.GrpcServer, d)
}

// NewDebugService creates a new grpc service using config data.
func NewDebugService(conState api.ConservativeState, host api.NetworkIdentity) *DebugService {
	return &DebugService{
		conState: conState,
		identity: host,
	}
}

// Accounts returns current counter and balance for all accounts.
func (d DebugService) Accounts(_ context.Context, in *empty.Empty) (*pb.AccountsResponse, error) {
	log.Info("GRPC DebugServices.Accounts")

	accounts, err := d.conState.GetAllAccounts()
	if err != nil {
		log.Error("Failed to get all accounts from state: %s", err)
		return nil, status.Errorf(codes.Internal, "error fetching accounts state")
	}

	res := &pb.AccountsResponse{}

	for _, account := range accounts {
		state := &pb.AccountState{
			Counter: account.NextNonce,
			Balance: &pb.Amount{Value: account.Balance},
		}

		account := &pb.Account{
			AccountId:    &pb.AccountId{Address: account.Address.String()}, // Address is bech32 string, not a 0x hex string
			StateCurrent: state,
		}

		res.AccountWrapper = append(res.AccountWrapper, account)
	}

	return res, nil
}

// NetworkInfo query provides NetworkInfoResponse.
func (d DebugService) NetworkInfo(ctx context.Context, _ *empty.Empty) (*pb.NetworkInfoResponse, error) {
	return &pb.NetworkInfoResponse{Id: d.identity.ID().String()}, nil
}

// ProposalsStream streams all proposals confirmed by hare.
func (d DebugService) ProposalsStream(_ *emptypb.Empty, stream pb.DebugService_ProposalsStreamServer) error {
	sub := events.SubcribeProposals()
	if sub == nil {
		return status.Errorf(codes.FailedPrecondition, "event reporting is not enabled")
	}
	eventch, fullch := consumeEvents[events.EventProposal](stream.Context(), sub)
	// send empty header after subscribing to the channel.
	// this is optional but allows subscriber to wait until stream is fully initialized.
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return status.Errorf(codes.Unavailable, "can't send header")
	}
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-fullch:
			return status.Errorf(codes.Canceled, "buffer is full")
		case ev := <-eventch:
			if err := stream.Send(castEventProposal(&ev)); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		}
	}
}

func castEventProposal(ev *events.EventProposal) *pb.Proposal {
	proposal := &pb.Proposal{
		Id:      ev.Proposal.ID().Bytes(),
		Epoch:   &pb.SimpleInt{Value: uint64(ev.Proposal.Layer.GetEpoch())},
		Layer:   convertLayerID(ev.Proposal.Layer),
		Smesher: &pb.SmesherId{Id: ev.Proposal.SmesherID.Bytes()},
		Ballot:  ev.Proposal.Ballot.ID().Bytes(),
	}
	if ev.Proposal.EpochData != nil {
		proposal.EpochData = &pb.Proposal_Data{Data: &pb.EpochData{
			Beacon: ev.Proposal.EpochData.Beacon.Bytes(),
		}}
	} else {
		proposal.EpochData = &pb.Proposal_Reference{Reference: ev.Proposal.RefBallot.Bytes()}
	}
	proposal.Status = pb.Proposal_Status(ev.Status)
	proposal.Eligibilities = make([]*pb.Eligibility, 0, len(ev.Proposal.EligibilityProofs))
	for _, el := range ev.Proposal.Ballot.EligibilityProofs {
		proposal.Eligibilities = append(proposal.Eligibilities, &pb.Eligibility{
			J:         el.J,
			Signature: el.Sig[:],
		})
	}
	return proposal
}

func (d DebugService) ActiveSet(ctx context.Context, req *pb.ActiveSetRequest) (*pb.ActiveSetResponse, error) {
	panic("NOT IMPLEMENTED")
}
