package identity

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type tag int

const (
	tagRetrying tag = iota
	tagWaitforATXSynced
	tagWaitForPoetRegistrationWindow
	tagPoetChallengeReady
	tagPoetRegistered
	tagWaitForPoetRoundEnd
	tagPoetProofReceived
	tagGeneratingPostProof
	tagPostProofReady
	tagAtxReady
	tagAtxBroadcasted
	tagProposalBuildFailed
	tagProposalPublishFailed
	tagProposalPublished
	tagEligibile
)

type serializableStateInfo struct {
	// The tag is used to determine how to deserialize the raw state.
	Tag          tag
	PublishEpoch *types.EpochID
	Time         time.Time

	RawState json.RawMessage
}

func tagToState(s tag) State {
	switch s {
	case tagRetrying:
		return new(Retrying)
	case tagWaitforATXSynced:
		return new(WaitForATXSynced)
	case tagWaitForPoetRegistrationWindow:
		return new(WaitingForPoetRegistrationWindow)
	case tagPoetChallengeReady:
		return new(PoetChallengeReady)
	case tagPoetRegistered:
		return new(PoetRegistered)
	case tagWaitForPoetRoundEnd:
		return new(WaitForPoetRoundEnd)
	case tagPoetProofReceived:
		return new(PoetProofReceived)
	case tagGeneratingPostProof:
		return new(GeneratingPostProof)
	case tagPostProofReady:
		return new(PostProofReady)
	case tagAtxReady:
		return new(ATXReady)
	case tagAtxBroadcasted:
		return new(ATXBroadcasted)
	case tagProposalBuildFailed:
		return new(ProposalBuildFailed)
	case tagProposalPublishFailed:
		return new(ProposalPublishFailed)
	case tagProposalPublished:
		return new(ProposalPublished)
	case tagEligibile:
		return new(Eligible)
	default:
		panic(fmt.Sprintf("missing implementation for %v", s))
	}
}

func stateToTag(s State) tag {
	switch s.(type) {
	case *Retrying:
		return tagRetrying
	case *WaitForATXSynced:
		return tagWaitforATXSynced
	case *WaitingForPoetRegistrationWindow:
		return tagWaitForPoetRegistrationWindow
	case *PoetChallengeReady:
		return tagPoetChallengeReady
	case *PoetRegistered:
		return tagPoetRegistered
	case *WaitForPoetRoundEnd:
		return tagWaitForPoetRoundEnd
	case *PoetProofReceived:
		return tagPoetProofReceived
	case *GeneratingPostProof:
		return tagGeneratingPostProof
	case *PostProofReady:
		return tagPostProofReady
	case *ATXReady:
		return tagAtxReady
	case *ATXBroadcasted:
		return tagAtxBroadcasted
	case *ProposalBuildFailed:
		return tagProposalBuildFailed
	case *ProposalPublishFailed:
		return tagProposalPublishFailed
	case *ProposalPublished:
		return tagProposalPublished
	case *Eligible:
		return tagEligibile
	default:
		panic(fmt.Sprintf("missing implementation for %T", s))
	}
}

func unmarshalState(b []byte) (*StateInfo, error) {
	var s serializableStateInfo
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	info := &StateInfo{
		State:        tagToState(s.Tag),
		PublishEpoch: s.PublishEpoch,
		Time:         s.Time,
	}
	if err := json.Unmarshal(s.RawState, info.State); err != nil {
		return nil, err
	}
	return info, nil
}

func marshalState(state *StateInfo) ([]byte, error) {
	rawState, err := json.Marshal(state.State)
	if err != nil {
		return nil, err
	}
	s := serializableStateInfo{
		Tag:          stateToTag(state.State),
		PublishEpoch: state.PublishEpoch,
		Time:         state.Time,
		RawState:     json.RawMessage(rawState),
	}
	return json.Marshal(s)
}
