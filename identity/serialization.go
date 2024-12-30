package identity

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type state int

const (
	stateRetrying state = iota
	stateWaitforATXSynced
	stateWaitForPoetRegistrationWindow
	statePoetChallengeReady
	statePoetRegistered
	stateWaitForPoetRoundEnd
	statePoetProofReceived
	stateGeneratingPostProof
	statePostProofReady
	stateAtxReady
	stateAtxBroadcasted
	stateProposalBuildFailed
	stateProposalPublishFailed
	stateProposalPublished
	stateEligibile
)

type serializableStateInfo struct {
	Desc         state
	PublishEpoch *types.EpochID
	Time         time.Time

	RawState json.RawMessage
}

func stateDescToState(s state) State {
	switch s {
	case stateRetrying:
		return new(Retrying)
	case stateWaitforATXSynced:
		return new(WaitForATXSynced)
	case stateWaitForPoetRegistrationWindow:
		return new(WaitingForPoetRegistrationWindow)
	case statePoetChallengeReady:
		return new(PoetChallengeReady)
	case statePoetRegistered:
		return new(PoetRegistered)
	case stateWaitForPoetRoundEnd:
		return new(WaitForPoetRoundEnd)
	case statePoetProofReceived:
		return new(PoetProofReceived)
	case stateGeneratingPostProof:
		return new(GeneratingPostProof)
	case statePostProofReady:
		return new(PostProofReady)
	case stateAtxReady:
		return new(ATXReady)
	case stateAtxBroadcasted:
		return new(ATXBroadcasted)
	case stateProposalBuildFailed:
		return new(ProposalBuildFailed)
	case stateProposalPublishFailed:
		return new(ProposalPublishFailed)
	case stateProposalPublished:
		return new(ProposalPublished)
	case stateEligibile:
		return new(Eligible)
	default:
		panic(fmt.Sprintf("missing implementation for %v", s))
	}
}

func stateToDesc(s State) state {
	switch s.(type) {
	case *Retrying:
		return stateRetrying
	case *WaitForATXSynced:
		return stateWaitforATXSynced
	case *WaitingForPoetRegistrationWindow:
		return stateWaitForPoetRegistrationWindow
	case *PoetChallengeReady:
		return statePoetChallengeReady
	case *PoetRegistered:
		return statePoetRegistered
	case *WaitForPoetRoundEnd:
		return stateWaitForPoetRoundEnd
	case *PoetProofReceived:
		return statePoetProofReceived
	case *GeneratingPostProof:
		return stateGeneratingPostProof
	case *PostProofReady:
		return statePostProofReady
	case *ATXReady:
		return stateAtxReady
	case *ATXBroadcasted:
		return stateAtxBroadcasted
	case *ProposalBuildFailed:
		return stateProposalBuildFailed
	case *ProposalPublishFailed:
		return stateProposalPublishFailed
	case *ProposalPublished:
		return stateProposalPublished
	case *Eligible:
		return stateEligibile
	default:
		panic(fmt.Sprintf("missing implementation for %T", s))
	}
}

func unmarshalState(b []byte) (*StateInfo, error) {
	var s serializableStateInfo
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	state := stateDescToState(s.Desc)
	if err := json.Unmarshal(s.RawState, state); err != nil {
		return nil, err
	}
	return &StateInfo{
		State:        state,
		PublishEpoch: s.PublishEpoch,
		Time:         s.Time,
	}, nil
}

func marshalState(state *StateInfo) ([]byte, error) {
	rawState, err := json.Marshal(state.State)
	if err != nil {
		return nil, err
	}
	s := serializableStateInfo{
		Desc:         stateToDesc(state.State),
		PublishEpoch: state.PublishEpoch,
		Time:         state.Time,
		RawState:     json.RawMessage(rawState),
	}
	return json.Marshal(s)
}
