package grpc

import (
	"fmt"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/poet/broadcaster/pb"
)

// StartMining start post init followed by publication of atxs and blocks
func (s NodeService) StartMining(ctx context.Context, message *pb.InitPost) (*pb.SimpleMessage, error) {
	log.Info("GRPC StartMining msg")
	addr, err := types.StringToAddress(message.Coinbase)
	if err != nil {
		return nil, err
	}
	err = s.Mining.StartPost(addr, message.LogicalDrive, message.CommitmentSize)
	if err != nil {
		return nil, err
	}
	return &pb.SimpleMessage{Value: "ok"}, nil
}

// SetAwardsAddress sets the award address for which this miner will receive awards
func (s NodeService) SetAwardsAddress(ctx context.Context, id *pb.AccountId) (*pb.SimpleMessage, error) {
	log.Info("GRPC SetAwardsAddress msg")
	addr := types.HexToAddress(id.Address)
	s.Mining.SetCoinbaseAccount(addr)
	return &pb.SimpleMessage{Value: "ok"}, nil
}

// GetMiningStats returns post creation status, coinbase address and post data directory
func (s NodeService) GetMiningStats(ctx context.Context, empty *empty.Empty) (*pb.MiningStats, error) {
	//todo: we should review if this RPC is necessary
	log.Info("GRPC GetInitProgress msg")
	stat, remainingBytes, coinbase, dataDir := s.Mining.MiningStats()
	return &pb.MiningStats{
		DataDir:        dataDir,
		Status:         int32(stat),
		Coinbase:       coinbase,
		RemainingBytes: remainingBytes,
	}, nil
}

// GetUpcomingAwards returns the id of layers at which this miner will receive rewards
func (s NodeService) GetUpcomingAwards(ctx context.Context, empty *empty.Empty) (*pb.EligibleLayers, error) {
	log.Info("GRPC GetUpcomingAwards msg")
	layers := s.Oracle.GetEligibleLayers()
	ly := make([]uint64, 0, len(layers))
	for _, l := range layers {
		ly = append(ly, uint64(l))
	}
	return &pb.EligibleLayers{Layers: ly}, nil
}

// ResetPost removed post commitment for this miner
func (s NodeService) ResetPost(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC ResetPost msg")
	stat, _, _, _ := s.Mining.MiningStats()
	if stat == activation.InitInProgress {
		return nil, fmt.Errorf("cannot reset, init in progress")
	}
	err := s.Post.Reset()
	if err != nil {
		return nil, err
	}
	return &pb.SimpleMessage{Value: "ok"}, nil
}

