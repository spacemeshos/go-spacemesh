package grpcserver

import (
	"context"
	"fmt"
	"sort"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/libp2p/go-libp2p/core/network"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
)

// DebugService exposes global state data, output from the STF.
type DebugService struct {
	db       *sql.Database
	conState conservativeState
	netInfo  networkInfo
	oracle   oracle
	loggers  map[string]*zap.AtomicLevel
}

// RegisterService registers this service with a grpc server instance.
func (d DebugService) RegisterService(server *grpc.Server) {
	pb.RegisterDebugServiceServer(server, d)
}

func (s DebugService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterDebugServiceHandlerServer(context.Background(), mux, s)
}

// String returns the name of this service.
func (d DebugService) String() string {
	return "DebugService"
}

// NewDebugService creates a new grpc service using config data.
func NewDebugService(db *sql.Database, conState conservativeState, host networkInfo, oracle oracle,
	loggers map[string]*zap.AtomicLevel,
) *DebugService {
	return &DebugService{
		db:       db,
		conState: conState,
		netInfo:  host,
		oracle:   oracle,
		loggers:  loggers,
	}
}

// Accounts returns current counter and balance for all accounts.
func (d DebugService) Accounts(ctx context.Context, in *pb.AccountsRequest) (*pb.AccountsResponse, error) {
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
		ctxzap.Error(ctx, " Failed to get all accounts from state", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "error fetching accounts state")
	}

	res := &pb.AccountsResponse{}

	for _, account := range accts {
		state := &pb.AccountState{
			Counter: account.NextNonce,
			Balance: &pb.Amount{Value: account.Balance},
		}

		account := &pb.Account{
			AccountId: &pb.AccountId{
				Address: account.Address.String(),
			}, // Address is bech32 string, not a 0x hex string
			StateCurrent: state,
		}

		res.AccountWrapper = append(res.AccountWrapper, account)
	}

	return res, nil
}

// NetworkInfo query provides NetworkInfoResponse.
func (d DebugService) NetworkInfo(ctx context.Context, _ *emptypb.Empty) (*pb.NetworkInfoResponse, error) {
	resp := &pb.NetworkInfoResponse{Id: d.netInfo.ID().String()}
	for _, a := range d.netInfo.ListenAddresses() {
		resp.ListenAddresses = append(resp.ListenAddresses, a.String())
	}
	sort.Strings(resp.ListenAddresses)
	for _, a := range d.netInfo.KnownAddresses() {
		resp.KnownAddresses = append(resp.KnownAddresses, a.String())
	}
	sort.Strings(resp.KnownAddresses)
	udpNATType, tcpNATType := d.netInfo.NATDeviceType()
	resp.NatTypeUdp = convertNATType(udpNATType)
	resp.NatTypeTcp = convertNATType(tcpNATType)
	resp.Reachability = convertReachability(d.netInfo.Reachability())
	resp.DhtServerEnabled = d.netInfo.DHTServerEnabled()
	resp.Stats = make(map[string]*pb.DataStats)
	for _, proto := range d.netInfo.PeerInfo().Protocols() {
		s := d.netInfo.PeerInfo().EnsureProtoStats(proto)
		ds := &pb.DataStats{
			BytesSent:     uint64(s.BytesSent()),
			BytesReceived: uint64(s.BytesReceived()),
		}
		sr1, sr2 := s.SendRate(1), s.SendRate(2)
		if sr1 != 0 || sr2 != 0 {
			ds.SendRate = []uint64{
				uint64(s.SendRate(1)),
				uint64(s.SendRate(2)),
			}
		}
		rr1, rr2 := s.RecvRate(1), s.RecvRate(2)
		if rr1 != 0 || rr2 != 0 {
			ds.RecvRate = []uint64{
				uint64(s.RecvRate(1)),
				uint64(s.RecvRate(2)),
			}
		}
		resp.Stats[string(proto)] = ds
	}

	return resp, nil
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
	sub := events.SubscribeProposals()
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

func (d DebugService) ChangeLogLevel(ctx context.Context, req *pb.ChangeLogLevelRequest) (*emptypb.Empty, error) {
	level, err := zap.ParseAtomicLevel(req.GetLevel())
	if err != nil {
		return nil, fmt.Errorf("parse level: %w", err)
	}

	if req.GetModule() == "*" {
		for _, logger := range d.loggers {
			logger.SetLevel(level.Level())
		}
		return nil, nil
	}

	logger, ok := d.loggers[req.GetModule()]
	if !ok {
		return nil, fmt.Errorf("cannot find logger %v", req.GetModule())
	}

	logger.SetLevel(level.Level())

	return &emptypb.Empty{}, nil
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

func convertNATType(natType network.NATDeviceType) pb.NetworkInfoResponse_NATType {
	switch natType {
	case network.NATDeviceTypeCone:
		return pb.NetworkInfoResponse_Cone
	case network.NATDeviceTypeSymmetric:
		return pb.NetworkInfoResponse_Symmetric
	default:
		return pb.NetworkInfoResponse_NATTypeUnknown
	}
}

func convertReachability(r network.Reachability) pb.NetworkInfoResponse_Reachability {
	switch r {
	case network.ReachabilityPublic:
		return pb.NetworkInfoResponse_Public
	case network.ReachabilityPrivate:
		return pb.NetworkInfoResponse_Private
	default:
		return pb.NetworkInfoResponse_ReachabilityUnknown
	}
}
