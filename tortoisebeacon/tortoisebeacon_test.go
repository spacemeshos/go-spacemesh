package tortoisebeacon

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func TestTortoiseBeacon(t *testing.T) {
	requirer := require.New(t)
	conf := TestConfig()
	mr := &mockMessageReceiver{
		messages: generateMessageList(),
	}
	ms := mockMessageSender{rnd: rand.New(rand.NewSource(time.Now().UnixNano()))}
	mbc := mockBeaconCalculator{}
	mwc := mockWeakCoin{}
	mwmp := mockWeakCoinPublisher{}

	atxPool := activation.NewAtxMemPool()
	logger := log.NewDefault("TortoiseBeacon")
	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(10) * time.Second
	types.SetLayersPerEpoch(1)
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, log.NewDefault("clock"))
	clock.StartNotifying()
	ticker := clock.Subscribe()

	tb := New(conf, mr, ms, mbc, atxPool, nil, mwc, mwmp, ticker, logger)
	requirer.NotNil(tb)

	err := tb.Start()
	requirer.NoError(err)

	epoch := types.EpochID(2)

	t.Logf("Awaiting epoch %v", epoch)
	awaitEpoch(clock, epoch)

	t.Logf("Waiting for beacon value for epoch %v", epoch)

	err = tb.Wait(epoch)
	requirer.NoError(err)

	t.Logf("Beacon value for epoch %v is ready", epoch)

	v, err := tb.Get(epoch)
	requirer.NoError(err)

	expected := "0x2b32db6c2c0a6235fb1397e8225ea85e0f0e6e8c7b126d0016ccbde0e667151e"
	requirer.Equal(expected, v.String())

	requirer.NoError(tb.Close())
}

func awaitEpoch(clock *timesync.TimeClock, epoch types.EpochID) {
	layerTicker := clock.Subscribe()

	for layer := range layerTicker {
		// Wait until required epoch passes.
		if layer.GetEpoch() > epoch {
			return
		}
	}
}

func generateMessageList() []Message {
	// TODO: generate messages properly
	messages := []Message{
		NewProposalMessage(0, []types.ATXID{*types.EmptyATXID}),
		NewVotingMessage(0, 0, []types.Hash32{hashATXList([]types.ATXID{*types.EmptyATXID})}, []types.Hash32{}),
		NewVotingMessage(0, 0, []types.Hash32{hashATXList([]types.ATXID{*types.EmptyATXID})}, []types.Hash32{}),
		NewVotingMessage(0, 0, []types.Hash32{hashATXList([]types.ATXID{*types.EmptyATXID})}, []types.Hash32{}),
		NewVotingMessage(0, 0, []types.Hash32{hashATXList([]types.ATXID{*types.EmptyATXID})}, []types.Hash32{}),
		NewVotingMessage(0, 0, []types.Hash32{hashATXList([]types.ATXID{*types.EmptyATXID})}, []types.Hash32{}),
		NewVotingMessage(0, 0, []types.Hash32{hashATXList([]types.ATXID{*types.EmptyATXID})}, []types.Hash32{}),
		NewVotingMessage(0, 0, []types.Hash32{hashATXList([]types.ATXID{*types.EmptyATXID})}, []types.Hash32{}),
		NewVotingMessage(0, 0, []types.Hash32{hashATXList([]types.ATXID{*types.EmptyATXID})}, []types.Hash32{}),
	}

	return messages
}

func TestTortoiseBeacon_classifyMessage(t *testing.T) {
	r := require.New(t)

	epoch := types.EpochID(3)
	round := 4

	tb := TortoiseBeacon{
		currentRounds: map[types.EpochID]int{
			epoch: round,
		},
	}

	tt := []struct {
		name    string
		round   int
		msgType MessageType
	}{
		{"Timely", 3, TimelyMessage},
		{"Delayed", 2, DelayedMessage},
		{"Late", 1, LateMessage},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			m := vote{round: tc.round}
			result := tb.classifyMessage(m, epoch)
			r.Equal(tc.msgType, result)
		})
	}
}

func Test_hashATXList(t *testing.T) {
	r := require.New(t)

	tt := []struct {
		name     string
		atxList  []types.ATXID
		expected string
	}{
		{
			name:     "Case 1",
			atxList:  []types.ATXID{types.ATXID(types.CalcHash32([]byte("1")))},
			expected: "0x9c2e4d8fe97d881430de4e754b4205b9c27ce96715231cffc4337340cb110280",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := hashATXList(tc.atxList).String()
			r.Equal(tc.expected, result)
		})
	}
}
