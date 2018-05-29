package timesync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"testing"
	"time"
)

func TestCheckSystemClockDrift(t *testing.T) {
	drift, err := CheckSystemClockDrift()
	t.Log("Checking system clock drift from NTP")
	if drift < -nodeconfig.TimeConfigValues.MaxAllowedDrift || drift > nodeconfig.TimeConfigValues.MaxAllowedDrift {
		assert.NotNil(t, err, fmt.Sprintf("Didn't get that drift exceedes. %s", err))
	} else {
		assert.Nil(t, err, fmt.Sprintf("Drift is ok"))
	}
}

func TestNtpPacket_Time(t *testing.T) {
	ntpDate := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	since := time.Since(ntpDate)
	sysTime := time.Now().UTC()
	p := NtpPacket{
		TxTimeSec:  uint32(since.Seconds()),
		TxTimeFrac: uint32(since.Nanoseconds()),
	}

	// This will fail when our conversion doesn't function right because ntp count 70 years more than unix
	assert.True(t, p.Time().Year() == sysTime.Year(), "Converted and system time should be the same year")
}

func generateRandomDurations() (sortableDurations, error) {
	sd := sortableDurations{}
	rand := 0
	for rand < 10 {
		rand = int(crypto.GetRandomUInt32(50))
	}
	for i := 0; i < rand; i++ {
		randomSecs := crypto.GetRandomUInt32(100)
		sd = append(sd, time.Duration(time.Second*time.Duration(randomSecs)))
	}
	return sd, nil
}

func TestSortableDurations_RemoveExtremes(t *testing.T) {
	// Generate slice of len (random) of random durations (with max 100 seconds)
	durations, err := generateRandomDurations()
	if err != nil {
		t.Error("could'nt create durations")
	}
	baseLen := len(durations)
	durations.RemoveExtremes()
	assert.True(t, len(durations) == baseLen-2, "Slice length should be reduced by two ")
}

// TODO : add more tests for rest of the functions
