package identity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

func RequireEqual(t *testing.T, expected, value *StateInfo) {
	t.Helper()
	// NOTE: The `require` doesn't support comparing time.Time after marhsaling
	// (the monotonic counter is dropped in the process of serialization).
	// We need to compare the values manually for some types.
	// See: https://github.com/stretchr/testify/issues/502
	switch e := expected.State.(type) {
	case *WaitForPoetRoundEnd:
		v := value.State.(*WaitForPoetRoundEnd)
		require.EqualValues(t, e.RoundEnd.UnixNano(), v.RoundEnd.UnixNano())
		require.EqualValues(t, e.PublishEpochEnd.UnixNano(), v.PublishEpochEnd.UnixNano())
	case *PoetRegistered:
		v := value.State.(*PoetRegistered)
		require.Len(t, v.Registrations, len(e.Registrations))
		for i, reg := range v.Registrations {
			require.Equal(t, e.Registrations[i].Address, reg.Address)
			require.Equal(t, e.Registrations[i].RoundID, reg.RoundID)
			require.Equal(t, e.Registrations[i].ChallengeHash, reg.ChallengeHash)
			require.Equal(t, e.Registrations[i].RoundEnd.UnixNano(), reg.RoundEnd.UnixNano())

		}
	default:
		require.EqualValues(t, expected.State, value.State)
	}
	require.EqualValues(t, expected.Time.UnixNano(), value.Time.UnixNano())
	require.EqualValues(t, expected.PublishEpoch, value.PublishEpoch)
}

func testRoundTrip(t *testing.T, state State) {
	t.Helper()
	t.Parallel()
	epoch := types.EpochID(8)
	time := time.Now()
	expected := &StateInfo{
		State:        state,
		PublishEpoch: &epoch,
		Time:         time,
	}
	bytes, err := marshalState(expected)
	require.NoError(t, err)
	zaptest.NewLogger(t).Error("encoded", zap.String("json", string(bytes)))

	decoded, err := unmarshalState(bytes)
	require.NoError(t, err)
	RequireEqual(t, expected, decoded)
}

func TestSerializationRoundTrip(t *testing.T) {
	t.Run("state Retrying", func(t *testing.T) {
		testRoundTrip(t, &Retrying{})
	})
	t.Run("state WaitForATXSynced", func(t *testing.T) {
		testRoundTrip(t, &WaitForATXSynced{})
	})
	t.Run("state WaitingForPoetRegistrationWindow", func(t *testing.T) {
		testRoundTrip(t, &WaitingForPoetRegistrationWindow{})
	})
	t.Run("state PoetChallengeReady", func(t *testing.T) {
		testRoundTrip(t, &PoetChallengeReady{})
	})
	t.Run("state PoetRegistered", func(t *testing.T) {
		testRoundTrip(t, &PoetRegistered{
			Registrations: []nipost.PoETRegistration{
				{
					ChallengeHash: types.RandomHash(),
					Address:       "http://poet",
					RoundID:       "123",
					RoundEnd:      time.Now(),
				},
			},
		})
	})
	t.Run("state WaitForPoetRoundEnd", func(t *testing.T) {
		testRoundTrip(t, &WaitForPoetRoundEnd{
			RoundEnd:        time.Now().Add(time.Hour),
			PublishEpochEnd: time.Now().Add(time.Hour * 10),
		})
	})
	t.Run("state PoetProofReceived", func(t *testing.T) {
		testRoundTrip(t, &PoetProofReceived{
			PoetUrl: "http://poet.sm",
		})
	})
	t.Run("state GeneratingPostProof", func(t *testing.T) {
		testRoundTrip(t, &GeneratingPostProof{})
	})
	t.Run("state PostProofReady", func(t *testing.T) {
		testRoundTrip(t, &PostProofReady{})
	})
	t.Run("state AtxReady", func(t *testing.T) {
		testRoundTrip(t, &ATXReady{})
	})
	t.Run("state ATXBroadcasted", func(t *testing.T) {
		testRoundTrip(t, &WaitingForPoetRegistrationWindow{})
	})
	t.Run("state ProposalBuildFailed", func(t *testing.T) {
		testRoundTrip(t, &ProposalBuildFailed{
			ErrorMsg: "foo failed",
			Layer:    56,
		})
	})
	t.Run("state ProposalPublishFailed", func(t *testing.T) {
		testRoundTrip(t, &ProposalPublishFailed{
			ErrorMsg: "ooops",
			Proposal: types.RandomProposalID(),
			Layer:    99,
		})
	})
	t.Run("state Eligible", func(t *testing.T) {
		testRoundTrip(t, &Eligible{
			Layers: map[types.LayerID][]types.VotingEligibility{
				54: {
					{
						J:   1,
						Sig: types.RandomVrfSignature(),
					},
				},
			},
		})
	})
}

func TestStateSerializationErrors(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		_, err := unmarshalState([]byte(`{invalid json`))
		require.Error(t, err)
	})

	t.Run("invalid state type", func(t *testing.T) {
		require.Panics(t, func() { unmarshalState([]byte(`{"Tag": 999, "Time": "2023-01-01T00:00:00Z"}`)) })
	})

	t.Run("invalid state data", func(t *testing.T) {
		// Valid desc but invalid state data
		data := []byte(`{"Desc": 0, "Time": "2023-01-01T00:00:00Z", "RawState": "{invalid}"}`)
		_, err := unmarshalState(data)
		require.Error(t, err)
	})
}
