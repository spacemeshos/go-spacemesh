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

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
)

// DebugService exposes global state data, output from the STF.
type DebugService struct {
	db       *sql.Database
	conState conservativeState
	identity networkIdentity
	oracle   oracle
}

// RegisterService registers this service with a grpc server instance.
func (d DebugService) RegisterService(server *Server) {
	pb.RegisterDebugServiceServer(server.GrpcServer, d)
}

// NewDebugService creates a new grpc service using config data.
func NewDebugService(db *sql.Database, conState conservativeState, host networkIdentity, oracle oracle) *DebugService {
	return &DebugService{
		db:       db,
		conState: conState,
		identity: host,
		oracle:   oracle,
	}
}

// Accounts returns current counter and balance for all accounts.
func (d DebugService) Accounts(_ context.Context, in *pb.AccountsRequest) (*pb.AccountsResponse, error) {
	log.Info("GRPC DebugServices.Accounts")

	var (
		accts []*types.Account
		err   error
	)
	if in.Layer == 0 {
		accts, err = d.conState.GetAllAccounts()
	} else {
		accts, err = accounts.Snapshot(d.db, types.LayerID(in.Layer))
	}
	if err != nil {
		log.Error("Failed to get all accounts from state: %s", err)
		return nil, status.Errorf(codes.Internal, "error fetching accounts state")
	}

	res := &pb.AccountsResponse{}

	for _, account := range accts {
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

// ActiveSet query provides hare active set for the specified epoch.
func (d DebugService) ActiveSet(ctx context.Context, req *pb.ActiveSetRequest) (*pb.ActiveSetResponse, error) {
	actives, err := d.oracle.ActiveSet(ctx, types.EpochID(req.Epoch))
	if err != nil {
		return nil, status.Errorf(codes.Internal, fmt.Sprintf("active set for epoch %d: %s", req.Epoch, err.Error()))
	}
	resp := &pb.ActiveSetResponse{}
	for _, atxid := range actives {
		resp.Ids = append(resp.Ids, &pb.ActivationId{Id: atxid.Bytes()})
	}
	return resp, nil
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
		Epoch:   &pb.EpochNumber{Number: ev.Proposal.Layer.GetEpoch().Uint32()},
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
