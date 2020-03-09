package timesync

import (
	"fmt"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/stretchr/testify/assert"
)

func TestCheckSystemClockDrift(t *testing.T) {
	drift, err := CheckSystemClockDrift()
	t.Log("Checking system clock drift from NTP")
	if drift < -config.TimeConfigValues.MaxAllowedDrift || drift > config.TimeConfigValues.MaxAllowedDrift {
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

func TestCheckMessageDrift(t *testing.T) {
	reqtime := time.Now()
	for i := 0; reqtime.Add(time.Second * time.Duration(i)).Before(reqtime.Add(MaxAllowedMessageDrift)); i++ {
		fu := reqtime.Add(time.Second * time.Duration(i))
		pa := reqtime.Add(-(time.Second * time.Duration(i)))

		assert.True(t, CheckMessageDrift(fu.Unix()))
		assert.True(t, CheckMessageDrift(pa.Unix()))
	}

	msgtime := time.Now().Add(-(MaxAllowedMessageDrift + time.Minute))
	// check message that was sent too far in the past
	on := CheckMessageDrift(msgtime.Unix())
	assert.False(t, on)

	msgtime = time.Now().Add(MaxAllowedMessageDrift + time.Minute)
	// check message that was sent too far in the future
	on = CheckMessageDrift(msgtime.Unix())
	assert.False(t, on)
}

func MockntpRequest(server string, rq *NtpPacket) (time.Time, time.Duration, *NtpPacket, error) {
	return time.Now(), 0, &NtpPacket{}, nil
}

func Test_queryNtpServerZeroTime(t *testing.T) {
	ntpFunc = MockntpRequest
	defer func() {
		ntpFunc = ntpRequest
	}()

	errChan := make(chan error)
	resChan := make(chan time.Duration)
	go queryNtpServer("mock", resChan, errChan)

	select {
	case err := <-errChan:
		assert.NotNil(t, err)
	case <-time.After(1 * time.Second):
		assert.Fail(t, "error not received")
	}

}
