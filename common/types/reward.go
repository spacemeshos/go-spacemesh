package types

// Reward is a virtual reward transaction, which the node keeps track of for the gRPC api.
type Reward struct {
	Layer               LayerID
	TotalReward         uint64
	LayerRewardEstimate uint64
}
