package model

import (
	"context"
	"errors"
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const layerSize = 50

func newCore(rng *rand.Rand, logger log.Log) *core {
	c := &core{
		logger:  logger,
		rng:     rng,
		meshdb:  newMeshDB(logger),
		atxdb:   newAtxDB(logger, types.GetLayersPerEpoch()),
		beacons: newBeaconStore(),
		units:   10,
		signer:  signing.NewEdSignerFromRand(rng),
	}
	cfg := tortoise.DefaultConfig()
	cfg.LayerSize = layerSize
	c.tortoise = tortoise.New(c.meshdb, c.atxdb, c.beacons,
		tortoise.WithLogger(logger),
		tortoise.WithConfig(cfg),
	)
	return c
}

// core state machine.
type core struct {
	logger log.Log
	rng    *rand.Rand

	meshdb   *mesh.DB
	atxdb    *activation.DB
	beacons  *beaconStore
	tortoise *tortoise.Tortoise

	// generated on setup
	units  uint32
	signer signing.Signer

	// set in the first layer of each epoch
	refBallot     *types.BallotID
	weight        uint64
	eligibilities uint32

	// set at the end of the epoch (EventLayerEnd for the last layer of the epoch)
	atx types.ATXID
}

// OnEvent receive blocks, atx, input vector, beacon, coinflip and store them.
// Generate atx at the end of each epoch.
// Generate block at the start of every layer.
func (c *core) OnEvent(event Event) []Event {
	switch ev := event.(type) {
	case EventLayerStart:
		// TODO(dshulyak) produce ballot according to eligibilities
		if !ev.LayerID.After(types.GetEffectiveGenesis()) {
			return nil
		}
		if c.refBallot == nil {
			total, _, err := c.atxdb.GetEpochWeight(ev.LayerID.GetEpoch())
			if err != nil {
				panic(err)
			}
			c.eligibilities = max(uint32(c.weight*50/total), 1)
		}
		var events []Event
		for i := uint32(0); i < c.eligibilities; i++ {
			votes, err := c.tortoise.BaseBallot(context.TODO())
			if err != nil {
				panic(err)
			}
			ballot := &types.Ballot{}
			ballot.LayerIndex = ev.LayerID
			ballot.Votes = *votes
			ballot.AtxID = c.atx
			ballot.EligibilityProof.J = i

			if c.refBallot != nil {
				ballot.RefBallot = *c.refBallot
			} else {
				_, activeset, err := c.atxdb.GetEpochWeight(ev.LayerID.GetEpoch())
				if err != nil {
					panic(err)
				}
				beacon, err := c.beacons.GetBeacon(ev.LayerID.GetEpoch())
				if err != nil {
					beacon = types.Beacon{}
					c.rng.Read(beacon[:])
					c.beacons.StoreBeacon(ev.LayerID.GetEpoch(), beacon)
				}
				ballot.EpochData = &types.EpochData{
					ActiveSet: activeset,
					Beacon:    beacon,
				}
			}
			ballot.Signature = c.signer.Sign(ballot.Bytes())
			ballot.Initialize()

			if c.refBallot == nil {
				id := ballot.ID()
				c.refBallot = &id
			}
			events = append(events, EventBallot{Ballot: ballot})
		}
		return events
	case EventLayerEnd:
		c.tortoise.HandleIncomingLayer(context.TODO(), ev.LayerID)

		if ev.LayerID.GetEpoch() == ev.LayerID.Add(1).GetEpoch() {
			return nil
		}

		nipost := types.NIPostChallenge{
			NodeID:     types.NodeID{Key: c.signer.PublicKey().String()},
			StartTick:  1,
			EndTick:    2,
			PubLayerID: ev.LayerID,
		}
		atx := types.NewActivationTx(nipost, types.BytesToAddress(c.signer.PublicKey().Bytes()), nil, uint(c.units), nil)

		c.refBallot = nil
		c.atx = atx.ID()
		c.weight = atx.GetWeight()

		return []Event{
			EventAtx{Atx: atx},
		}
	case EventBlock:
		ids, err := c.meshdb.LayerBlockIds(ev.Block.LayerIndex)
		if errors.Is(err, database.ErrNotFound) || len(ids) == 0 {
			c.meshdb.SaveHareConsensusOutput(context.Background(), ev.Block.LayerIndex, ev.Block.ID())
		}
		c.meshdb.AddBlock(ev.Block)
	case EventBallot:
		c.meshdb.AddBallot(ev.Ballot)
	case EventAtx:
		c.atxdb.StoreAtx(context.TODO(), ev.Atx.TargetEpoch(), ev.Atx)
	case EventBeacon:
		c.beacons.StoreBeacon(ev.EpochID, ev.Beacon)
	case EventCoinflip:
		c.meshdb.RecordCoinflip(context.TODO(), ev.LayerID, ev.Coinflip)
	}
	return nil
}

func newBeaconStore() *beaconStore {
	return &beaconStore{
		beacons: map[types.EpochID]types.Beacon{},
	}
}

type beaconStore struct {
	beacons map[types.EpochID]types.Beacon
}

func (b *beaconStore) GetBeacon(eid types.EpochID) (types.Beacon, error) {
	beacon, exist := b.beacons[eid-1]
	if !exist {
		return types.Beacon{}, database.ErrNotFound
	}
	return beacon, nil
}

func (b *beaconStore) StoreBeacon(eid types.EpochID, beacon types.Beacon) {
	b.beacons[eid] = beacon
}

func newAtxDB(logger log.Log, layersPerEpoch uint32) *activation.DB {
	db := database.NewMemDatabase()
	return activation.NewDB(db, nil, nil, layersPerEpoch, types.ATXID{1}, nil, logger)
}

func newMeshDB(logger log.Log) *mesh.DB {
	return mesh.NewMemMeshDB(logger)
}

func max(i, j uint32) uint32 {
	if i > j {
		return i
	}
	return j
}
