package activation

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/natefinch/atomic"
	"github.com/stretchr/testify/require"
)

func TestWriteCRC(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		crc      []byte
		validCRC bool
	}{
		{
			name:     "validCRC",
			data:     []byte("spacemesh"),
			crc:      []byte{0xC6, 0x10, 0x72, 0xDF, 0x52, 0xD7, 0x74, 0x0E},
			validCRC: true,
		}, {
			name:     "badCRC",
			data:     []byte("spacemesh"),
			crc:      []byte{0xC7, 0x11, 0x73, 0xE0, 0x53, 0xD8, 0x7, 0x0F},
			validCRC: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.name)
			require.NoError(t, write(path, tt.data))

			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, tt.validCRC, bytes.Equal(raw, append(tt.data, tt.crc...)))
		})
	}
}

func TestReadCRC(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		crc    []byte
		errMsg string
	}{
		{
			name:   "validCRC",
			data:   []byte("spacemesh"),
			crc:    []byte{0xC6, 0x10, 0x72, 0xDF, 0x52, 0xD7, 0x74, 0x0E},
			errMsg: "",
		}, {
			name:   "badCRC",
			data:   []byte("spacemesh"),
			crc:    []byte{0xC7, 0x11, 0x73, 0xE0, 0x53, 0xD8, 0x75, 0x0F},
			errMsg: "wrong checksum 0xC71173E053D8750F, computed 0xC61072DF52D7740E",
		}, {
			name:   "file too short",
			data:   []byte("123"),
			crc:    []byte{},
			errMsg: "too small",
		}, {
			name:   "file empty",
			data:   []byte(""),
			crc:    []byte{0xC6, 0x10, 0x72, 0xDF, 0x52, 0xD7, 0x74, 0x0E},
			errMsg: "wrong checksum 0xC61072DF52D7740E, computed 0x0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			path := filepath.Join(tmp, tt.name)
			err := atomic.WriteFile(path, bytes.NewReader(append(tt.data, tt.crc...)))
			require.NoError(t, err)

			data, err := read(path)
			if tt.errMsg == "" {
				require.NoError(t, err)
				require.True(t, bytes.Equal(data, tt.data))
			} else {
				require.ErrorContains(t, err, tt.errMsg)
				require.Nil(t, data)
			}
		})
	}
}

func TestWriteRecover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test")
	data := []byte("secret")
	require.NoError(t, write(path, data))
	got, err := read(path)
	require.NoError(t, err)
	require.True(t, bytes.Equal(data, got))
}

func TestCorrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test")
	data := []byte("secret")
	require.NoError(t, write(path, data))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data = append([]byte("new "), data...)
	err = atomic.WriteFile(path, bytes.NewReader(data))
	require.NoError(t, err)
	got, err := read(path)
	require.ErrorContains(t, err, "wrong checksum")
	require.Nil(t, got)
}
