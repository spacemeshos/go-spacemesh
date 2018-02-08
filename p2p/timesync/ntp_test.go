package timesync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/assert"
	"testing"
	"time"
)

func TestCheckSystemClockDrift(t *testing.T) {
	drift, err := CheckSystemClockDrift()
	t.Log("Checking system clock drift from NTP")
	if drift < -MAX_ALLOWED_DRIFT || drift > MAX_ALLOWED_DRIFT {
		assert.NotNil(t, err, fmt.Sprintf("Didn't get that drift exceedes", err))
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

// TODO : add more tests for rest of the functions
