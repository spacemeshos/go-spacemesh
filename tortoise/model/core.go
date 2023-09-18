package model

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const (
	layerSize = 50
	units     = 10
)

func newCore(rng *rand.Rand, id string, logger log.Log) *core {
	cdb := datastore.NewCachedDB(sql.InMemory(), logger)
	sig, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
	if err != nil {
		panic(err)
	}
	c := &core{
		id:      id,
		logger:  logger,
		rng:     rng,
		cdb:     cdb,
		beacons: newBeaconStore(),
		units:   units,
		signer:  sig,
	}
	cfg := tortoise.DefaultConfig()
	cfg.LayerSize = layerSize
	c.tortoise, err = tortoise.New(
		tortoise.WithLogger(logger.Named("trtl")),
		tortoise.WithConfig(cfg),
	)
	if err != nil {
		panic(err)
	}
	return c
}

// core state machine.
type core struct {
	id     string
	logger log.Log
	rng    *rand.Rand

	cdb      *datastore.CachedDB
	beacons  *beaconStore
	tortoise *tortoise.Tortoise

	// generated on setup
	units  uint32
	signer *signing.EdSigner

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
			total, _, err := c.cdb.GetEpochWeight(ev.LayerID.GetEpoch())
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
		ballot.Layer = ev.LayerID
		ballot.Votes = votes.Votes
		ballot.OpinionHash = votes.Hash
		ballot.AtxID = c.atx
		for i := uint32(0); i < c.eligibilities; i++ {
			ballot.EligibilityProofs = append(ballot.EligibilityProofs, types.VotingEligibility{J: i})
		}
		if c.refBallot != nil {
			ballot.RefBallot = *c.refBallot
		} else {
			beacon, err := c.beacons.GetBeacon(ev.LayerID.GetEpoch())
			if err != nil {
				beacon = types.Beacon{}
				c.rng.Read(beacon[:])
				c.beacons.StoreBeacon(ev.LayerID.GetEpoch(), beacon)
			}
			ballot.EpochData = &types.EpochData{
				ActiveSetHash:    types.Hash32{1, 2, 3},
				Beacon:           beacon,
				EligibilityCount: c.eligibilities,
			}
		}
		ballot.Signature = c.signer.Sign(signing.BALLOT, ballot.SignedBytes())
		ballot.SmesherID = c.signer.NodeID()
		ballot.Initialize()
		if c.refBallot == nil {
			id := ballot.ID()
			c.refBallot = &id
		}
		m.Send(MessageBallot{Ballot: ballot})
	case MessageLayerEnd:
		if ev.LayerID.After(types.GetEffectiveGenesis()) {
			tortoise.RecoverLayer(context.Background(), c.tortoise, c.cdb, c.beacons, ev.LayerID, ev.LayerID, ev.LayerID)
			m.Notify(EventVerified{ID: c.id, Verified: c.tortoise.LatestComplete(), Layer: ev.LayerID})
		}

		if ev.LayerID.GetEpoch() == ev.LayerID.Add(1).GetEpoch() {
			return
		}

		nipost := types.NIPostChallenge{
			PublishEpoch: ev.LayerID.GetEpoch(),
		}
		addr := types.GenerateAddress(c.signer.PublicKey().Bytes())
		atx := types.NewActivationTx(nipost, addr, nil, c.units, nil)
		if err := activation.SignAndFinalizeAtx(c.signer, atx); err != nil {
			c.logger.With().Fatal("failed to sign atx", log.Err(err))
		}
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		vAtx, err := atx.Verify(1, 2)
		if err != nil {
			c.logger.With().Fatal("failed to verify atx", log.Err(err))
		}

		c.refBallot = nil
		c.atx = vAtx.ID()
		c.weight = vAtx.GetWeight()

		m.Send(MessageAtx{Atx: vAtx.ActivationTx})
	case MessageBlock:
		ids, err := blocks.IDsInLayer(c.cdb, ev.Block.LayerIndex)
		if errors.Is(err, sql.ErrNotFound) || len(ids) == 0 {
			certificates.SetHareOutput(c.cdb, ev.Block.LayerIndex, ev.Block.ID())
		}
		blocks.Add(c.cdb, ev.Block)
	case MessageBallot:
		ballots.Add(c.cdb, ev.Ballot)
	case MessageAtx:
		vAtx, err := ev.Atx.Verify(1, 2)
		if err != nil {
			panic(err)
		}
		atxs.Add(c.cdb, vAtx)
	case MessageBeacon:
		c.beacons.StoreBeacon(ev.EpochID, ev.Beacon)
	case MessageCoinflip:
		layers.SetWeakCoin(c.cdb, ev.LayerID, ev.Coinflip)
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
		return types.Beacon{}, sql.ErrNotFound
	}
	return beacon, nil
}

func (b *beaconStore) StoreBeacon(eid types.EpochID, beacon types.Beacon) {
	b.beacons[eid] = beacon
}
