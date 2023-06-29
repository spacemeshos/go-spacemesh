package events

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type eventEnum int

func (e eventEnum) String() string {
	return eventEnumStrings[e]
}

var eventEnumStrings = [...]string{
	"error",
	"beacon",
	"init_start",
	"init_complete",
	"poet_wait_start",
	"poet_wait_complete",
	"atx_published",
	"proposal_eligibilities",
	"proposal_created",
	"identity_equivocated",
}

const (
	eventError eventEnum = iota
	eventBeacon
	eventInitStart
	eventInitStop
	eventWaitPoetStart
	eventWaitPoetComplete
	eventAtxPublished
	eventProposalEligibilities
	eventProposalCreated
	identityEquivocated
)

type Event struct {
	Timestamp *time.Time `json:"ts"`
	Enum      int        `json:"type"`
	Event     any        `json:"event,inline,omitempty"`
	Persist   bool       `json:"-"`
}

type EventBeacon struct {
	Beacon types.Beacon `json:"beacon"`
}
