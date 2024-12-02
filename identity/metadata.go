package identity

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

type RetryingStateMetadata struct {
	Error error
}

func WithRetryingState(metadata *RetryingStateMetadata) StateInfoMetadata {
	return func(info *StateInfo) {
		info.RetryingState = metadata
	}
}

type PoetRegisteredStateMetadata struct {
	Registrations []nipost.PoETRegistration
}

func WithPoetRegisteredState(metadata *PoetRegisteredStateMetadata) StateInfoMetadata {
	return func(info *StateInfo) {
		info.PoetRegisteredState = metadata
	}
}

type WaitForPoetRoundEndStateMetadata struct {
	RoundEnd        time.Time
	PublishEpochEnd time.Time
}

func WithWaitForPoetRoundEndState(metadata *WaitForPoetRoundEndStateMetadata) StateInfoMetadata {
	return func(info *StateInfo) {
		info.WaitForPoetRoundEndState = metadata
	}
}

type PoetProofReceivedStateMetadata struct {
	PoetUrl string
}

func WithPoetProofReceivedState(metadata *PoetProofReceivedStateMetadata) StateInfoMetadata {
	return func(info *StateInfo) {
		info.PoetProofReceivedState = metadata
	}
}

type AtxBroadcastedStateMetadata struct {
	AtxId types.ATXID
}

func WithAtxBroadcastedState(metadata *AtxBroadcastedStateMetadata) StateInfoMetadata {
	return func(info *StateInfo) {
		info.AtxBroadcastedState = metadata
	}
}

type ProposalPublishedStateMetadata struct {
	Proposal types.ProposalID
	Layer    types.LayerID
}
