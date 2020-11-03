package grpcserver

import (
	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GlobalStateService exposes global state data, output from the STF
type DebugService struct {
	Mesh        api.TxAPI
	Mempool     api.MempoolAPI

}

// RegisterService registers this service with a grpc server instance
func (d DebugService) RegisterService(server *Server) {
	pb.RegisterDebugServiceServer(server.GrpcServer, d)
}

// DebugService creates a new grpc service using config data.
func NewDebugService(tx api.TxAPI, mempool api.MempoolAPI) *GlobalStateService {
	return &GlobalStateService{
		Mesh:        tx,
		Mempool:     mempool,
	}
}

func (d DebugService) getProjection(curCounter, curBalance uint64, addr types.Address) (counter, balance uint64, err error) {
	counter, balance, err = d.Mesh.GetProjection(addr, curCounter, curBalance)
	if err != nil {
		return 0, 0, err
	}
	counter, balance = d.Mempool.GetProjection(addr, counter, balance)
	return counter, balance, nil
}

func (d DebugService) getAccount(addr types.Address) (acct *pb.Account, err error) {
	balanceActual := d.Mesh.GetBalance(addr)
	counterActual := d.Mesh.GetNonce(addr)
	counterProjected, balanceProjected, err := d.getProjection(counterActual, balanceActual, addr)
	if err != nil {
		return nil, err
	}
	return &pb.Account{
		AccountId: &pb.AccountId{Address: addr.Bytes()},
		StateCurrent: &pb.AccountState{
			Counter: counterActual,
			Balance: &pb.Amount{Value: balanceActual},
		},
		StateProjected: &pb.AccountState{
			Counter: counterProjected,
			Balance: &pb.Amount{Value: balanceProjected},
		},
	}, nil
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
			Balance: &pb.Amount{Value: accountData.Balance.Uint64()},
		}

		account := &pb.Account{
			AccountId:    &pb.AccountId{Address: []byte(address)},
			StateCurrent: state,
		}

		res.AccountWrapper = append(res.AccountWrapper, account)
	}

	return res, nil
}

