package model

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const (
	layerSize = 50
	units     = 10
)

func newCore(rng *rand.Rand, id string, logger *zap.Logger) *core {
	cdb := datastore.NewCachedDB(statesql.InMemory(), logger)
	sig, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
	if err != nil {
		panic(err)
	}
	c := &core{
		id:      id,
		logger:  logger,
		rng:     rng,
		cdb:     cdb,
		units:   units,
		signer:  sig,
		atxdata: atxsdata.New(),
	}
	cfg := tortoise.DefaultConfig()
	cfg.LayerSize = layerSize
	c.tortoise, err = tortoise.New(
		c.atxdata,
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
	logger *zap.Logger
	rng    *rand.Rand

	cdb      *datastore.CachedDB
	atxdata  *atxsdata.Data
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
			total := uint64(0)
			c.atxdata.IterateInEpoch(ev.LayerID.GetEpoch(), func(_ types.ATXID, atx *atxsdata.ATX) {
				total += atx.Weight
			})
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
			beacon, err := beacons.Get(c.cdb, ev.LayerID.GetEpoch())
			if err != nil {
				beacon = types.Beacon{}
				c.rng.Read(beacon[:])
				beacons.Set(c.cdb, ev.LayerID.GetEpoch(), beacon)
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
			tortoise.RecoverLayer(context.Background(),
				c.tortoise,
				c.cdb.Database,
				c.atxdata,
				ev.LayerID,
				c.tortoise.OnBallot,
			)
			c.tortoise.TallyVotes(context.Background(), ev.LayerID)
			m.Notify(EventVerified{ID: c.id, Verified: c.tortoise.LatestComplete(), Layer: ev.LayerID})
		}

		if ev.LayerID.GetEpoch() == ev.LayerID.Add(1).GetEpoch() {
			return
		}

		atx := &types.ActivationTx{
			PublishEpoch:   ev.LayerID.GetEpoch(),
			NumUnits:       c.units,
			Coinbase:       types.GenerateAddress(c.signer.PublicKey().Bytes()),
			SmesherID:      c.signer.NodeID(),
			BaseTickHeight: 1,
			TickCount:      2,
			Weight:         uint64(c.units) * 2,
		}
		atx.SetID(types.RandomATXID())
		atx.SetReceived(time.Now())
		c.refBallot = nil
		c.atx = atx.ID()
		c.weight = atx.Weight

		m.Send(MessageAtx{Atx: atx})
	case MessageBlock:
		ids, err := blocks.IDsInLayer(c.cdb, ev.Block.LayerIndex)
		if errors.Is(err, sql.ErrNotFound) || len(ids) == 0 {
			certificates.SetHareOutput(c.cdb, ev.Block.LayerIndex, ev.Block.ID())
		}
		blocks.Add(c.cdb, ev.Block)
	case MessageBallot:
		ballots.Add(c.cdb, ev.Ballot)
	case MessageAtx:
		ev.Atx.BaseTickHeight = 1
		ev.Atx.TickCount = 2
		atxs.Add(c.cdb, ev.Atx, types.AtxBlob{})
		malicious, err := identities.IsMalicious(c.cdb, ev.Atx.SmesherID)
		if err != nil {
			c.logger.Fatal("failed is malicious lookup", zap.Error(err))
		}
		c.atxdata.AddFromAtx(ev.Atx, malicious)
	case MessageBeacon:
		beacons.Add(c.cdb, ev.EpochID+1, ev.Beacon)
	case MessageCoinflip:
		layers.SetWeakCoin(c.cdb, ev.LayerID, ev.Coinflip)
	}
}
