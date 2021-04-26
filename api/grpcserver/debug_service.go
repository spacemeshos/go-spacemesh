package grpcserver

import (
	"context"

	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DebugService exposes global state data, output from the STF
type DebugService struct {
	Mesh api.TxAPI
}

// RegisterService registers this service with a grpc server instance
func (d DebugService) RegisterService(server *Server) {
	pb.RegisterDebugServiceServer(server.GrpcServer, d)
}

// NewDebugService creates a new grpc service using config data.
func NewDebugService(tx api.TxAPI) *DebugService {
	return &DebugService{
		Mesh: tx,
	}
}

// Accounts returns current counter and balance for all accounts
func (d DebugService) Accounts(_ context.Context, in *empty.Empty) (*pb.AccountsResponse, error) {
	log.Info("GRPC DebugServices.Accounts")

	accounts, err := d.Mesh.GetAllAccounts()
	if err != nil {
		log.Error("Failed to get all accounts from state: %s", err)
		return nil, status.Errorf(codes.Internal, "error fetching accounts state")
	}

	res := &pb.AccountsResponse{}

	for address, accountData := range accounts.Accounts {
		state := &pb.AccountState{
			Counter: accountData.Nonce,
			Balance: &pb.Amount{Value: accountData.Balance},
		}

		account := &pb.Account{
			AccountId:    &pb.AccountId{Address: util.FromHex(address)}, // Address is raw account bytes, not a 0x hex string
			StateCurrent: state,
		}

		res.AccountWrapper = append(res.AccountWrapper, account)
	}

	return res, nil
}
