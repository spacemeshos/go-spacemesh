package fptree

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

func verifyPrefix(t *testing.T, p prefix) {
	for i := (p.len() + 7) / 8; i < prefixBytes; i++ {
		require.Zero(t, p.b[i], "p.bs[%d]", i)
	}
}

func TestPrefix(t *testing.T) {
	for _, tc := range []struct {
		p       prefix
		s       string
		left    prefix
		right   prefix
		shift   prefix
		minID   string
		idAfter string
	}{
		{
			p:       emptyPrefix,
			s:       "<0>",
			left:    prefix{b: [prefixBytes]byte{0}, l: 1},
			right:   prefix{b: [prefixBytes]byte{0x80}, l: 1},
			minID:   "0000000000000000000000000000000000000000000000000000000000000000",
			idAfter: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			p:       prefix{b: [prefixBytes]byte{0}, l: 1},
			s:       "<1:0>",
			left:    prefix{b: [prefixBytes]byte{0}, l: 2},
			right:   prefix{b: [prefixBytes]byte{0x40}, l: 2},
			shift:   emptyPrefix,
			minID:   "0000000000000000000000000000000000000000000000000000000000000000",
			idAfter: "8000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			p:       prefix{b: [prefixBytes]byte{0x80}, l: 1},
			s:       "<1:1>",
			left:    prefix{b: [prefixBytes]byte{0x80}, l: 2},
			right:   prefix{b: [prefixBytes]byte{0xc0}, l: 2},
			shift:   emptyPrefix,
			minID:   "8000000000000000000000000000000000000000000000000000000000000000",
			idAfter: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			p:       prefix{b: [prefixBytes]byte{0}, l: 2},
			s:       "<2:00>",
			left:    prefix{b: [prefixBytes]byte{0}, l: 3},
			right:   prefix{b: [prefixBytes]byte{0x20}, l: 3},
			shift:   prefix{b: [prefixBytes]byte{0}, l: 1},
			minID:   "0000000000000000000000000000000000000000000000000000000000000000",
			idAfter: "4000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			p:       prefix{b: [prefixBytes]byte{0x40}, l: 2},
			s:       "<2:01>",
			left:    prefix{b: [prefixBytes]byte{0x40}, l: 3},
			right:   prefix{b: [prefixBytes]byte{0x60}, l: 3},
			shift:   prefix{b: [prefixBytes]byte{0x80}, l: 1},
			minID:   "4000000000000000000000000000000000000000000000000000000000000000",
			idAfter: "8000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			p:       prefix{b: [prefixBytes]byte{0x80}, l: 2},
			s:       "<2:10>",
			left:    prefix{b: [prefixBytes]byte{0x80}, l: 3},
			right:   prefix{b: [prefixBytes]byte{0xa0}, l: 3},
			shift:   prefix{b: [prefixBytes]byte{0}, l: 1},
			minID:   "8000000000000000000000000000000000000000000000000000000000000000",
			idAfter: "c000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			p:       prefix{b: [prefixBytes]byte{0xc0}, l: 2},
			s:       "<2:11>",
			left:    prefix{b: [prefixBytes]byte{0xc0}, l: 3},
			right:   prefix{b: [prefixBytes]byte{0xe0}, l: 3},
			shift:   prefix{b: [prefixBytes]byte{0x80}, l: 1},
			minID:   "c000000000000000000000000000000000000000000000000000000000000000",
			idAfter: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			p:       prefix{b: [prefixBytes]byte{0xff, 0xff, 0xff}, l: 24},
			s:       "<24:111111111111111111111111>",
			left:    prefix{b: [prefixBytes]byte{0xff, 0xff, 0xff, 0}, l: 25},
			right:   prefix{b: [prefixBytes]byte{0xff, 0xff, 0xff, 0x80}, l: 25},
			shift:   prefix{b: [prefixBytes]byte{0xff, 0xff, 0xfe}, l: 23},
			minID:   "ffffff0000000000000000000000000000000000000000000000000000000000",
			idAfter: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			p:       prefix{b: [prefixBytes]byte{0xff, 0xff, 0xff, 0}, l: 25},
			s:       "<25:1111111111111111111111110>",
			left:    prefix{b: [prefixBytes]byte{0xff, 0xff, 0xff, 0}, l: 26},
			right:   prefix{b: [prefixBytes]byte{0xff, 0xff, 0xff, 0x40}, l: 26},
			shift:   prefix{b: [prefixBytes]byte{0xff, 0xff, 0xfe}, l: 24},
			minID:   "ffffff0000000000000000000000000000000000000000000000000000000000",
			idAfter: "ffffff8000000000000000000000000000000000000000000000000000000000",
		},
	} {
		t.Run(fmt.Sprint(tc.p), func(t *testing.T) {
			require.Equal(t, tc.s, tc.p.String())
			require.Equal(t, tc.left, tc.p.left())
			verifyPrefix(t, tc.p.left())
			require.Equal(t, tc.right, tc.p.right())
			verifyPrefix(t, tc.p.right())
			if tc.p != emptyPrefix {
				require.Equal(t, tc.shift, tc.p.shift())
				verifyPrefix(t, tc.p.shift())
			}

			minID := make(rangesync.KeyBytes, 32)
			tc.p.minID(minID)
			require.Equal(t, tc.minID, minID.String())

			idAfter := make(rangesync.KeyBytes, 32)
			tc.p.idAfter(idAfter)
			require.Equal(t, tc.idAfter, idAfter.String())
		})
	}
}

func TestCommonPrefix(t *testing.T) {
	for _, tc := range []struct {
		a, b, p string
	}{
		{
			a: "0000000000000000000000000000000000000000000000000000000000000000",
			b: "8000000000000000000000000000000000000000000000000000000000000000",
			p: "<0>",
		},
		{
			a: "A000000000000000000000000000000000000000000000000000000000000000",
			b: "8000000000000000000000000000000000000000000000000000000000000000",
			p: "<2:10>",
		},
		{
			a: "A000000000000000000000000000000000000000000000000000000000000000",
			b: "A800000000000000000000000000000000000000000000000000000000000000",
			p: "<4:1010>",
		},
		{
			a: "ABCDEF1234567890000000000000000000000000000000000000000000000000",
			b: "ABCDEF1234567800000000000000000000000000000000000000000000000000",
			p: "<56:10101011110011011110111100010010001101000101011001111000>",
		},
		{
			a: "ABCDEF1234567890123456789ABCDEF000000000000000000000000000000000",
			b: "ABCDEF1234567890123456789ABCDEF000000000000000000000000000000000",
			p: "<96:1010101111001101111011110001001000110100010101100111100010010000" +
				"00010010001101000101011001111000>",
		},
	} {
		a := rangesync.MustParseHexKeyBytes(tc.a)
		b := rangesync.MustParseHexKeyBytes(tc.b)
		require.Equal(t, tc.p, commonPrefix(a, b).String())
		verifyPrefix(t, commonPrefix(a, b))
	}
}

func TestPreFirst0(t *testing.T) {
	for _, tc := range []struct {
		k, exp string
	}{
		{
			k:   "00000000",
			exp: "<0>",
		},
		{
			k:   "10000000",
			exp: "<0>",
		},
		{
			k:   "40000000",
			exp: "<0>",
		},
		{
			k:   "00040000",
			exp: "<0>",
		},
		{
			k:   "80000000",
			exp: "<1:1>",
		},
		{
			k:   "c0000000",
			exp: "<2:11>",
		},
		{
			k:   "cc000000",
			exp: "<2:11>",
		},
		{
			k:   "ffc00000",
			exp: "<10:1111111111>",
		},
		{
			k:   "ffffffff",
			exp: "<32:11111111111111111111111111111111>",
		},
	} {
		k := rangesync.MustParseHexKeyBytes(tc.k)
		require.Equal(t, tc.exp, preFirst0(k).String(), "k=%s", tc.k)
		verifyPrefix(t, preFirst0(k))
	}
}

func TestPreFirst1(t *testing.T) {
	for _, tc := range []struct {
		k, exp string
	}{
		{
			k:   "ffffffff",
			exp: "<0>",
		},
		{
			k:   "80000000",
			exp: "<0>",
		},
		{
			k:   "c0000000",
			exp: "<0>",
		},
		{
			k:   "ffffffc0",
			exp: "<0>",
		},
		{
			k:   "70000000",
			exp: "<1:0>",
		},
		{
			k:   "30000000",
			exp: "<2:00>",
		},
		{
			k:   "00300000",
			exp: "<10:0000000000>",
		},
		{
			k:   "00000000",
			exp: "<32:00000000000000000000000000000000>",
		},
	} {
		k := rangesync.MustParseHexKeyBytes(tc.k)
		require.Equal(t, tc.exp, preFirst1(k).String(), "k=%s", tc.k)
		verifyPrefix(t, preFirst1(k))
	}
}

func TestMatch(t *testing.T) {
	for _, tc := range []struct {
		k     string
		p     prefix
		match bool
	}{
		{
			k:     "12345678",
			p:     emptyPrefix,
			match: true,
		},
		{
			k:     "12345678",
			p:     prefix{l: 1},
			match: true,
		},
		{
			k:     "12345678",
			p:     prefix{l: 3},
			match: true,
		},
		{
			k:     "12345678",
			p:     prefix{l: 4},
			match: false,
		},
		{
			k:     "12345678",
			p:     prefix{b: [prefixBytes]byte{0x80}, l: 1},
			match: false,
		},
		{
			k:     "12345678",
			p:     prefix{b: [prefixBytes]byte{0x80}, l: 2},
			match: false,
		},
		{
			k:     "12345678",
			p:     prefix{b: [prefixBytes]byte{0x10}, l: 4},
			match: true,
		},
		{
			k:     "12345678",
			p:     prefix{b: [prefixBytes]byte{0x12, 0x34, 0x50}, l: 20},
			match: true,
		},
		{
			k:     "12345678",
			p:     prefix{b: [prefixBytes]byte{0x12, 0x34, 0x50}, l: 24},
			match: false,
		},
		{
			k:     "12345678",
			p:     prefix{b: [prefixBytes]byte{0x12, 0x34, 0x56, 0x78}, l: 32},
			match: true,
		},
	} {
		k := rangesync.MustParseHexKeyBytes(tc.k)
		require.Equal(t, tc.match, tc.p.match(k), "k=%s p=%s", tc.k, tc.p)
	}
}

func TestPrefixFromKeyBytes(t *testing.T) {
	p := prefixFromKeyBytes(rangesync.MustParseHexKeyBytes(
		"123456789abcdef0123456789abcdef111111111111111111111111111111111"))
	require.Equal(t,
		"<96:000100100011010001010110011110001001101010111100"+
			"110111101111000000010010001101000101011001111000>",
		p.String())
}
