package system

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate mockgen -package=mocks -destination=./mocks/beacons.go -source=./beacons.go

// BeaconCollector defines the interface that collect beacon values from Ballots.
type BeaconCollector interface {
	ReportBeaconFromBallot(types.EpochID, types.BallotID, types.Beacon, uint64)
}
