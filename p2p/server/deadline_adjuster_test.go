package server

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/libp2p/go-yamux/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/p2p/server/mocks"
)

func TestDeadlineAdjuster(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mocks.NewMockpeerStream(ctrl)
	clock := clockwork.NewFakeClock()

	readChunks := []string{"xy", "ABCD", "EF", "0123", "4567", "89"}
	writeChunks := []string{"foo", "abcd", "efgh", "ijk", "bbbc"}

	now := clock.Now()

	var sdCalls []any
	for _, n := range []int{11, 13, 15, 17, 19, 31} {
		sdCalls = append(sdCalls, s.EXPECT().
			SetDeadline(now.Add(time.Duration(n)*time.Second)).
			Return(nil))
	}
	gomock.InOrder(sdCalls...)

	// Capture the deadlines:
	// s.EXPECT().
	// 	SetDeadline(gomock.Any()).
	// 	DoAndReturn(func(dt time.Time) error {
	// 		t.Logf("deadline: +%v", dt.Sub(now))
	// 		return nil
	// 	}).
	// 	AnyTimes()

	var readCalls []any
	for _, str := range readChunks {
		chunk := []byte(str)
		readCalls = append(readCalls, s.EXPECT().
			Read(gomock.Any()).
			DoAndReturn(func(b []byte) (int, error) {
				clock.Advance(time.Second)
				copy(b, []byte(chunk))
				return len(chunk), nil
			}))
	}
	readCalls = append(readCalls, s.EXPECT().
		Read(gomock.Any()).
		DoAndReturn(func(b []byte) (int, error) {
			clock.Advance(10 * time.Second)
			return 1, yamux.ErrTimeout
		}))
	gomock.InOrder(readCalls...)

	var writeCalls []any
	for _, str := range writeChunks {
		chunk := []byte(str)
		writeCalls = append(writeCalls, s.EXPECT().
			Write(chunk).DoAndReturn(func([]byte) (int, error) {
			clock.Advance(time.Second)
			return len(chunk), nil
		}))
	}
	writeCalls = append(writeCalls, s.EXPECT().
		Write(gomock.Any()).
		DoAndReturn(func(b []byte) (int, error) {
			clock.Advance(10 * time.Second)
			return 2, yamux.ErrTimeout
		}))
	gomock.InOrder(writeCalls...)

	dadj := newDeadlineAdjuster(s, "test", 10*time.Second)
	dadj.clock = clock
	dadj.chunkSize = 4

	b := make([]byte, 2)
	n, err := dadj.Read(b)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, []byte("xy"), b)

	b = make([]byte, 10)
	n, err = dadj.Read(b) // short read
	require.NoError(t, err)
	require.Equal(t, 6, n)
	require.Equal(t, []byte("ABCDEF"), b[:n])

	b = make([]byte, 10)
	n, err = dadj.Read(b)
	require.NoError(t, err)
	require.Equal(t, 10, n)
	require.Equal(t, []byte("0123456789"), b)

	n, err = dadj.Write([]byte("foo"))
	require.NoError(t, err)
	require.Equal(t, 3, n)

	n, err = dadj.Write([]byte("abcdefghijk"))
	require.NoError(t, err)
	require.Equal(t, 11, n)

	b = make([]byte, 2)
	n, err = dadj.Read(b)
	require.Equal(t, 1, n)
	require.ErrorIs(t, err, yamux.ErrTimeout)
	require.ErrorContains(t, err, "19 bytes read, 14 bytes written, timeout 10s")

	n, err = dadj.Write([]byte("bbbcdef"))
	require.Equal(t, 6, n)
	require.ErrorIs(t, err, yamux.ErrTimeout)
	require.ErrorContains(t, err, "19 bytes read, 20 bytes written, timeout 10s")
}
