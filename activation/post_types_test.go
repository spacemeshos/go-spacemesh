package activation

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettingPowDifficulty(t *testing.T) {
	t.Parallel()

	expected := bytes.Repeat([]byte{0x01, 0x02, 0x03, 0x04}, 8)
	encoded := hex.EncodeToString(expected)
	t.Run("parse 32B hex", func(t *testing.T) {
		t.Parallel()
		d := PowDifficulty{}
		err := d.Set(encoded)
		require.NoError(t, err)
		require.Equal(t, expected, d[:])
	})
	t.Run("input too short", func(t *testing.T) {
		t.Parallel()
		d := PowDifficulty{}
		require.Error(t, d.Set("123"))
		require.Equal(t, PowDifficulty{}, d)
	})
	t.Run("input too long", func(t *testing.T) {
		t.Parallel()
		d := PowDifficulty{}
		require.Error(t, d.Set(hex.EncodeToString(bytes.Repeat([]byte{0x01}, 33))))
		require.Equal(t, PowDifficulty{}, d)
	})
	t.Run("not a hex string", func(t *testing.T) {
		t.Parallel()
		encoded := encoded[:len(encoded)-1] + "G"
		d := PowDifficulty{}
		require.Error(t, d.Set(encoded))
		require.Equal(t, PowDifficulty{}, d)
	})
}

func TestSettingProviderID(t *testing.T) {
	t.Parallel()

	t.Run("valid value", func(t *testing.T) {
		t.Parallel()
		id := new(PostProviderID)
		require.NoError(t, id.Set("1234"))
		require.Equal(t, uint32(1234), *id.Value())
	})
	t.Run("no value", func(t *testing.T) {
		t.Parallel()
		id := new(PostProviderID)
		require.NoError(t, id.Set(""))
		require.Nil(t, id.Value())
	})
	t.Run("not a number", func(t *testing.T) {
		t.Parallel()
		id := new(PostProviderID)
		require.Error(t, id.Set("asdf"))
		require.Nil(t, id.Value())
	})
	t.Run("negative", func(t *testing.T) {
		t.Parallel()
		id := new(PostProviderID)
		require.Error(t, id.Set("-1"))
		require.Nil(t, id.Value())
	})
}

func TestSettingPostPowFlags(t *testing.T) {
	t.Parallel()

	t.Run("valid value", func(t *testing.T) {
		t.Parallel()
		f := new(PostPowFlags)
		require.NoError(t, f.Set("123"))
		require.EqualValues(t, 123, *f)
	})
	t.Run("no value", func(t *testing.T) {
		t.Parallel()
		f := new(PostPowFlags)
		require.NoError(t, f.Set(""))
		require.EqualValues(t, 0, *f)
	})
	t.Run("not a number", func(t *testing.T) {
		t.Parallel()
		f := new(PostPowFlags)
		require.Error(t, f.Set("a123"))
	})
}

func TestSettingPostRandomXMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []PostRandomXMode{PostRandomXModeFast, PostRandomXModeLight} {
		t.Run(fmt.Sprintf("valid value %s", mode.String()), func(t *testing.T) {
			t.Parallel()
			m := new(PostRandomXMode)
			require.NoError(t, m.Set(mode.String()))
			require.Equal(t, mode, *m)
		})
	}
	t.Run("no value", func(t *testing.T) {
		t.Parallel()
		m := new(PostRandomXMode)
		require.Error(t, m.Set(""))
		require.Empty(t, m)
	})
	t.Run("not a valid value", func(t *testing.T) {
		t.Parallel()
		m := new(PostRandomXMode)
		require.Error(t, m.Set("asdf"))
		require.Empty(t, m)
	})
}
