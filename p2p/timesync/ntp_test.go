package timesync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/assert"
	"testing"
)

func TestCheckSystemClockDrift(t *testing.T) {
	err := CheckSystemClockDrift()
	assert.Nil(t, err, fmt.Sprintf("Error checking clock drift with NTP %v", err))
}

// TODO : add more tests for rest of the functions
