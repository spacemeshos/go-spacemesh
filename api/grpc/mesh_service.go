package grpc

import (
	"fmt"
	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"time"
)

// GetAccountTxs returns transactions that were applied and pending by this account
func (s NodeService) GetAccountTxs(ctx context.Context, txsSinceLayer *pb.GetTxsSinceLayer) (*pb.AccountTxs, error) {
	log.Debug("GRPC GetAccountTxs msg")

	currentPBase := s.Tx.LatestLayerInState()

	addr := types.HexToAddress(txsSinceLayer.Account.Address)
	minLayer := types.LayerID(txsSinceLayer.StartLayer)
	if minLayer > s.Tx.LatestLayer() {
		return &pb.AccountTxs{}, fmt.Errorf("invalid start layer")
	}

	txs := pb.AccountTxs{ValidatedLayer: currentPBase.Uint64()}

	meshTxIds := s.getTxIdsFromMesh(minLayer, addr)
	for _, txID := range meshTxIds {
		txs.Txs = append(txs.Txs, txID.String())
	}

	mempoolTxIds := s.TxMempool.GetTxIdsByAddress(addr)
	for _, txID := range mempoolTxIds {
		txs.Txs = append(txs.Txs, txID.String())
	}

	return &txs, nil
}

func (s NodeService) getTxIdsFromMesh(minLayer types.LayerID, addr types.Address) []types.TransactionID {
	var txIDs []types.TransactionID
	for layerID := minLayer; layerID < s.Tx.LatestLayer(); layerID++ {
		destTxIDs := s.Tx.GetTransactionsByDestination(layerID, addr)
		txIDs = append(txIDs, destTxIDs...)
		originTxIds := s.Tx.GetTransactionsByOrigin(layerID, addr)
		txIDs = append(txIDs, originTxIds...)
	}
	return txIDs
}

// GetAccountRewards returns the rewards for the provided account
func (s NodeService) GetAccountRewards(ctx context.Context, account *pb.AccountId) (*pb.AccountRewards, error) {
	log.Debug("GRPC GetAccountRewards msg")
	acc := types.HexToAddress(account.Address)

	rewards, err := s.Tx.GetRewards(acc)
	if err != nil {
		log.Error("failed to get rewards: %v", err)
		return nil, err
	}
	rewardsOut := pb.AccountRewards{}
	for _, x := range rewards {
		rewardsOut.Rewards = append(rewardsOut.Rewards, &pb.Reward{
			Layer:               x.Layer.Uint64(),
			TotalReward:         x.TotalReward,
			LayerRewardEstimate: x.LayerRewardEstimate,
		})
	}

	return &rewardsOut, nil
}

// GetGenesisTime returns the time at which this blockmesh has started
func (s NodeService) GetGenesisTime(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetGenesisTime msg")
	return &pb.SimpleMessage{Value: s.GenTime.GetGenesisTime().Format(time.RFC3339)}, nil
}

