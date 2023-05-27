package tracer

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
)

type ConfigTrace struct {
	Hdist                    uint32
	Zdist                    uint32
	WindowSize               uint32
	MaxExceptions            int
	BadBeaconVoteDelayLayers uint32
	LayerSize                uint32
	EpochSize                uint32
}

type WeakCoinTrace struct {
	Layer types.LayerID
	Coin  bool
}

type BeaconTrace struct {
	Epoch  types.EpochID
	Beacon types.Beacon
}

type DecodeBallotTrace struct {
	Ballot types.Ballot
	// TODO(dshulyk) also include decoding result
	Error string
}

type EncodeVotesTrace struct {
	Layer   types.LayerID
	Opinion types.Opinion
	Error   string
}

type TallyVotesTrace struct {
	Layer types.LayerID
}

type HareTrace struct {
	Layer types.LayerID
	Vote  types.BlockID
}

type ResultsTrace struct {
	From, To types.LayerID
	Error    string
	Results  []result.Layer
}

type MissingActiveSetTrace struct {
	Epoch             types.EpochID
	Request, Response []types.ATXID
}

type TracerImpl interface {
	OnStart(*ConfigTrace)
	OnAtx(*types.ActivationTxHeader)
	OnDecodeBallot(*DecodeBallotTrace)
	OnStoreBallot(types.BallotID)
	OnWeakCoin(*WeakCoinTrace)
	OnBeacon(*BeaconTrace)
	OnEncodeVotes(*EncodeVotesTrace)
	OnTallyVotes(*TallyVotesTrace)
	OnBlock(types.BlockHeader)
	OnValidBlock(header types.BlockHeader)
	OnHareOutput(*HareTrace)
	OnUpdates(*ResultsTrace)
	OnResults(*ResultsTrace)
	OnMissingActiveSet(*MissingActiveSetTrace)
}
