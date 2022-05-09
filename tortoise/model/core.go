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
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const (
	layerSize = 50
	units     = 10
)

func newCore(rng *rand.Rand, id string, logger log.Log) *core {
	db := sql.InMemory()
	mdb, err := mesh.NewPersistentMeshDB(db, logger)
	if err != nil {
		panic(err)
	}
	c := &core{
		id:      id,
		logger:  logger,
		rng:     rng,
		meshdb:  mdb,
		atxdb:   activation.NewDB(db, nil, types.GetLayersPerEpoch(), types.ATXID{1}, nil, logger),
		beacons: newBeaconStore(),
		units:   units,
		signer:  signing.NewEdSignerFromRand(rng),
	}
	cfg := tortoise.DefaultConfig()
	cfg.LayerSize = layerSize
	c.tortoise = tortoise.New(c.meshdb, c.atxdb, c.beacons, updater{c.meshdb},
		tortoise.WithLogger(logger.Named("trtl")),
		tortoise.WithConfig(cfg),
	)
	return c
}

// core state machine.
type core struct {
	id     string
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

	// set at the end of the epoch (MessageLayerEnd for the last layer of the epoch)
	atx types.ATXID
}

// OnMessage receive blocks, atx, input vector, beacon, coinflip and store them.
// Generate atx at the end of each epoch.
// Generate block at the start of every layer.
func (c *core) OnMessage(m Messenger, event Message) {
	switch ev := event.(type) {
	case MessageLayerStart:
		// TODO(dshulyak) produce ballot according to eligibilities
		if !ev.LayerID.After(types.GetEffectiveGenesis()) {
			return
		}
		if c.refBallot == nil {
			total, _, err := c.atxdb.GetEpochWeight(ev.LayerID.GetEpoch())
			if err != nil {
				panic(err)
			}
			c.eligibilities = max(uint32(c.weight*layerSize/total), 1)
		}
		votes, err := c.tortoise.EncodeVotes(context.TODO())
		if err != nil {
			panic(err)
		}
		ballot := &types.Ballot{}
		ballot.LayerIndex = ev.LayerID
		ballot.Votes = *votes
		ballot.AtxID = c.atx
		for i := uint32(0); i < c.eligibilities; i++ {
			ballot.EligibilityProofs = append(ballot.EligibilityProofs, types.VotingEligibilityProof{J: i})
		}
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
		m.Send(MessageBallot{Ballot: ballot})
	case MessageLayerEnd:
		if ev.LayerID.After(types.GetEffectiveGenesis()) {
			verified := c.tortoise.HandleIncomingLayer(context.TODO(), ev.LayerID)
			m.Notify(EventVerified{ID: c.id, Verified: verified, Layer: ev.LayerID})
		}

		if ev.LayerID.GetEpoch() == ev.LayerID.Add(1).GetEpoch() {
			return
		}

		nipost := types.NIPostChallenge{
			NodeID:     types.BytesToNodeID(c.signer.PublicKey().Bytes()),
			StartTick:  1,
			EndTick:    2,
			PubLayerID: ev.LayerID,
		}
		atx := types.NewActivationTx(nipost, types.BytesToAddress(c.signer.PublicKey().Bytes()), nil, uint(c.units), nil)

		c.refBallot = nil
		c.atx = atx.ID()
		c.weight = atx.GetWeight()

		m.Send(MessageAtx{Atx: atx})
	case MessageBlock:
		ids, err := c.meshdb.LayerBlockIds(ev.Block.LayerIndex)
		if errors.Is(err, database.ErrNotFound) || len(ids) == 0 {
			c.meshdb.SaveHareConsensusOutput(context.Background(), ev.Block.LayerIndex, ev.Block.ID())
		}
		c.meshdb.AddBlock(ev.Block)
	case MessageBallot:
		c.meshdb.AddBallot(ev.Ballot)
	case MessageAtx:
		c.atxdb.StoreAtx(context.TODO(), ev.Atx.TargetEpoch(), ev.Atx)
	case MessageBeacon:
		c.beacons.StoreBeacon(ev.EpochID, ev.Beacon)
	case MessageCoinflip:
		c.meshdb.RecordCoinflip(context.TODO(), ev.LayerID, ev.Coinflip)
	}
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

func max(i, j uint32) uint32 {
	if i > j {
		return i
	}
	return j
}

type updater struct {
	*mesh.DB
}

func (u updater) UpdateBlockValidity(bid types.BlockID, lid types.LayerID, rst bool) error {
	return u.SaveContextualValidity(bid, lid, rst)
}
