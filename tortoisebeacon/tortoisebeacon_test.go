package tortoisebeacon

import (
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
	conf := DefaultConfig()
	mr := &mockMessageReceiver{
		messages: generateMessageList(),
	}
	ms := mockMessageSender{}
	mbc := mockBeaconCalculator{}
	atxPool := activation.NewAtxMemPool()
	logger := log.NewDefault("TortoiseBeacon")
	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(2) * time.Second
	types.SetLayersPerEpoch(1)
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, log.NewDefault("clock"))
	clock.StartNotifying()
	ticker := clock.Subscribe()

	tb := New(conf, mr, ms, mbc, atxPool, nil, ticker, logger)
	requirer.NotNil(tb)

	err := tb.Start()
	requirer.NoError(err)

	time.Sleep(20 * time.Second)

	err = tb.Wait(10)
	requirer.NoError(err)

	v, err := tb.Get(10)
	expected := types.Hash32{227, 176, 196, 66, 152, 252, 28, 20, 154, 251, 244, 200, 153, 111, 185, 36, 39, 174, 65, 228, 100, 155, 147, 76, 164, 149, 153, 27, 120, 82, 184, 85}
	requirer.Equal(expected, v)

	requirer.NoError(tb.Close())
}

func generateMessageList() []Message {
	messages := make([]Message, 0) // TODO: fill
	m := NewProposalMessage(0, []types.ATXID{*types.EmptyATXID})
	messages = append(messages, m)

	return messages
}
