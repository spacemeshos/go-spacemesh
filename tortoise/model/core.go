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

func newCore(rng *rand.Rand, logger log.Log) *core {
	c := &core{
		logger:  logger,
		rng:     rng,
		meshdb:  newMeshDB(logger),
		atxdb:   newAtxDB(logger, types.GetLayersPerEpoch()),
		beacons: newBeaconStore(),
		units:   rng.Uint32(),
		signer:  signing.NewEdSignerFromRand(rng),
	}
	c.tortoise = tortoise.New(c.meshdb, c.atxdb, c.beacons, tortoise.WithLogger(logger))
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
	refBallot *types.BallotID

	// set at the end of the epoch (EventLayerEnd for the last layer of the epoch)
	atx types.ATXID
}

// OnEvent receive blocks, atx, input vector, beacon, coinflip and store them.
// Generate atx at the end of each epoch.
// Generate block at the start of every layer.
func (c *core) OnEvent(event Event) []Event {
	switch ev := event.(type) {
	case EventLayerStart:
		// TODO(dshulyak) procuce ballot according to eligibilities

		votes, err := c.tortoise.BaseBallot(context.TODO())
		if err != nil {
			panic(err)
		}
		ballot := &types.Ballot{}
		ballot.Votes = *votes

		ballot.AtxID = c.atx

		if c.refBallot != nil {
			ballot.RefBallot = *c.refBallot
		} else {
			_, activeset, err := c.atxdb.GetEpochWeight(ev.LayerID.GetEpoch())
			if err != nil {
				panic(err)
			}
			beacon, err := c.beacons.GetBeacon(ev.LayerID.GetEpoch())
			if err != nil {
				panic(err)
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
	case EventLayerEnd:
		c.tortoise.HandleIncomingLayer(context.TODO(), ev.LayerID)

		if ev.LayerID.GetEpoch() == ev.LayerID.Add(1).GetEpoch() {
			return nil
		}
		// last layer of the epoch has ended

		nipost := types.NIPostChallenge{
			NodeID:     types.NodeID{Key: c.signer.PublicKey().String()},
			StartTick:  1,
			EndTick:    2,
			PubLayerID: ev.LayerID,
		}
		atx := types.NewActivationTx(nipost, types.Address{}, nil, uint(c.units), nil)

		c.refBallot = nil
		c.atx = atx.ID()

		return []Event{
			EventAtx{Atx: atx},
		}
	case EventBlock:
		ids, err := c.meshdb.LayerBlockIds(ev.Block.LayerIndex)
		if errors.Is(err, database.ErrNotFound) || len(ids) == 0 {
			c.meshdb.SaveHareConsensusOutput(context.Background(), ev.Block.LayerIndex, ev.Block.ID())
		}
		c.meshdb.AddBlock(ev.Block)
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
