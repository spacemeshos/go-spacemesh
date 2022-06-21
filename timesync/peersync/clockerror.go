package peersync

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/errcode"
)

type clockError struct {
	err     error
	details clockErrorDetails
}

func (c clockError) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("code", errcode.ErrClockDrift)
	encoder.AddString("errmsg", c.err.Error())
	if err := encoder.AddObject("details", &c.details); err != nil {
		return fmt.Errorf("add object: %w", err)
	}

	return nil
}

func (c clockError) Error() string {
	return c.err.Error()
}

type clockErrorDetails struct {
	Drift time.Duration
}

func (c clockErrorDetails) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddDuration("drift", c.Drift)
	return nil
}
