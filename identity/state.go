package identity

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

type State any

type WaitForATXSynced struct{}

type Retrying struct {
	Error error
}

// poet.
type WaitingForPoetRegistrationWindow struct{}

// building nipost challenge.
type PoetChallengeReady struct{}

type PoetRegistered struct {
	Registrations []nipost.PoETRegistration
}

// 2w pass...
type WaitForPoetRoundEnd struct {
	RoundEnd        time.Time
	PublishEpochEnd time.Time
}

type PoetProofReceived struct {
	PoetUrl string
}

// post.
type GeneratingPostProof struct{}

type PostProofReady struct{}

// atx.
type ATXReady struct{}

type ATXBroadcasted struct {
	AtxId types.ATXID
}

// proposal.
type ProposalPublished struct {
	Proposal types.ProposalID
	Layer    types.LayerID
}
