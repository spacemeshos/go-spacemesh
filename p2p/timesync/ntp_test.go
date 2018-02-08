package timesync

import (
	"testing"
	"github.com/spacemeshos/go-spacemesh/assert"
	"fmt"
)

func TestCheckSystemClockDrift(t *testing.T) {
	err := CheckSystemClockDrift()
	assert.Nil(t, err, fmt.Sprintf("Error checking clock drift with NTP %v", err))
}

// TODO : add more tests to rest of the functions