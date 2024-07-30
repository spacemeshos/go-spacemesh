package dbsync

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

func TestPrefix(t *testing.T) {
	for _, tc := range []struct {
		p     prefix
		s     string
		bits  uint64
		len   int
		left  prefix
		right prefix
		shift prefix
		minID string
		maxID string
	}{
		{
			p:     0,
			s:     "<0>",
			len:   0,
			bits:  0,
			left:  0b0_000001,
			right: 0b1_000001,
			minID: "0000000000000000000000000000000000000000000000000000000000000000",
			maxID: "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b0_000001,
			s:     "<1:0>",
			len:   1,
			bits:  0,
			left:  0b00_000010,
			right: 0b01_000010,
			shift: 0,
			minID: "0000000000000000000000000000000000000000000000000000000000000000",
			maxID: "7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b1_000001,
			s:     "<1:1>",
			len:   1,
			bits:  1,
			left:  0b10_000010,
			right: 0b11_000010,
			shift: 0,
			minID: "8000000000000000000000000000000000000000000000000000000000000000",
			maxID: "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b00_000010,
			s:     "<2:00>",
			len:   2,
			bits:  0,
			left:  0b000_000011,
			right: 0b001_000011,
			shift: 0b0_000001,
			minID: "0000000000000000000000000000000000000000000000000000000000000000",
			maxID: "3FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b01_000010,
			s:     "<2:01>",
			len:   2,
			bits:  1,
			left:  0b010_000011,
			right: 0b011_000011,
			shift: 0b1_000001,
			minID: "4000000000000000000000000000000000000000000000000000000000000000",
			maxID: "7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b10_000010,
			s:     "<2:10>",
			len:   2,
			bits:  2,
			left:  0b100_000011,
			right: 0b101_000011,
			shift: 0b0_000001,
			minID: "8000000000000000000000000000000000000000000000000000000000000000",
			maxID: "BFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b11_000010,
			s:     "<2:11>",
			len:   2,
			bits:  3,
			left:  0b110_000011,
			right: 0b111_000011,
			shift: 0b1_000001,
			minID: "C000000000000000000000000000000000000000000000000000000000000000",
			maxID: "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0x3fffffd8,
			s:     "<24:111111111111111111111111>",
			len:   24,
			bits:  0xffffff,
			left:  0x7fffff99,
			right: 0x7fffffd9,
			shift: 0x1fffffd7,
			minID: "FFFFFF0000000000000000000000000000000000000000000000000000000000",
			maxID: "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0x7fffff99,
			s:     "<25:1111111111111111111111110>",
			len:   25,
			bits:  0x1fffffe,
			left:  0xffffff1a,
			right: 0xffffff5a,
			shift: 0x3fffff98,
			minID: "FFFFFF0000000000000000000000000000000000000000000000000000000000",
			maxID: "FFFFFF7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
	} {
		t.Run(fmt.Sprint(tc.p), func(t *testing.T) {
			require.Equal(t, tc.s, tc.p.String())
			require.Equal(t, tc.bits, tc.p.bits())
			require.Equal(t, tc.len, tc.p.len())
			require.Equal(t, tc.left, tc.p.left())
			require.Equal(t, tc.right, tc.p.right())
			if tc.p != 0 {
				require.Equal(t, tc.shift, tc.p.shift())
			}

			expMinID := types.HexToHash32(tc.minID)
			var minID types.Hash32
			tc.p.minID(minID[:])
			require.Equal(t, expMinID, minID)

			// QQQQQ: TBD: rm (probably with maxid fields?)
			// expMaxID := types.HexToHash32(tc.maxID)
			// var maxID types.Hash32
			// tc.p.maxID(maxID[:])
			// require.Equal(t, expMaxID, maxID)
		})
	}
}

func TestCommonPrefix(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		p    prefix
	}{
		{
			a: "0000000000000000000000000000000000000000000000000000000000000000",
			b: "8000000000000000000000000000000000000000000000000000000000000000",
			p: 0,
		},
		{
			a: "A000000000000000000000000000000000000000000000000000000000000000",
			b: "8000000000000000000000000000000000000000000000000000000000000000",
			p: 0b10_000010,
		},
		{
			a: "A000000000000000000000000000000000000000000000000000000000000000",
			b: "A800000000000000000000000000000000000000000000000000000000000000",
			p: 0b1010_000100,
		},
		{
			a: "ABCDEF1234567890000000000000000000000000000000000000000000000000",
			b: "ABCDEF1234567800000000000000000000000000000000000000000000000000",
			p: 0x2af37bc48d159e38,
		},
		{
			a: "ABCDEF1234567890123456789ABCDEF000000000000000000000000000000000",
			b: "ABCDEF1234567890123456789ABCDEF000000000000000000000000000000000",
			p: 0xabcdef12345678ba,
		},
	} {
		a := types.HexToHash32(tc.a)
		b := types.HexToHash32(tc.b)
		require.Equal(t, tc.p, commonPrefix(a[:], b[:]))
	}
}

type fakeIDDBStore struct {
	db sql.Database
	*sqlIDStore
}

var _ idStore = &fakeIDDBStore{}

const fakeIDQuery = "select id from foo where id >= ? order by id limit ?"

func newFakeATXIDStore(db sql.Database, maxDepth int) *fakeIDDBStore {
	return &fakeIDDBStore{db: db, sqlIDStore: newSQLIDStore(db, fakeIDQuery, 32)}
}

func (s *fakeIDDBStore) registerHash(h KeyBytes) error {
	if err := s.sqlIDStore.registerHash(h); err != nil {
		return err
	}
	_, err := s.db.Exec("insert into foo (id) values (?)",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, h)
		}, nil)
	return err
}

type idStoreFunc func(maxDepth int) idStore

func testFPTree(t *testing.T, makeIDStore idStoreFunc) {
	type rangeTestCase struct {
		xIdx, yIdx       int
		x, y             string
		limit            int
		fp               string
		count            uint32
		itype            int
		startIdx, endIdx int
	}
	for _, tc := range []struct {
		name     string
		maxDepth int
		ids      []string
		ranges   []rangeTestCase
		x, y     string
	}{
		{
			name:     "empty",
			maxDepth: 24,
			ids:      nil,
			ranges: []rangeTestCase{
				{
					x:        "123456789abcdef0000000000000000000000000000000000000000000000000",
					y:        "123456789abcdef0000000000000000000000000000000000000000000000000",
					limit:    -1,
					fp:       "000000000000000000000000",
					count:    0,
					itype:    0,
					startIdx: -1,
					endIdx:   -1,
				},
				{
					x:        "123456789abcdef0000000000000000000000000000000000000000000000000",
					y:        "123456789abcdef0000000000000000000000000000000000000000000000000",
					limit:    1,
					fp:       "000000000000000000000000",
					count:    0,
					itype:    0,
					startIdx: -1,
					endIdx:   -1,
				},
				{
					x:        "123456789abcdef0000000000000000000000000000000000000000000000000",
					y:        "223456789abcdef0000000000000000000000000000000000000000000000000",
					limit:    1,
					fp:       "000000000000000000000000",
					count:    0,
					itype:    -1,
					startIdx: -1,
					endIdx:   -1,
				},
				{
					x:        "223456789abcdef0000000000000000000000000000000000000000000000000",
					y:        "123456789abcdef0000000000000000000000000000000000000000000000000",
					limit:    1,
					fp:       "000000000000000000000000",
					count:    0,
					itype:    1,
					startIdx: -1,
					endIdx:   -1,
				},
			},
		},
		{
			name:     "ids1",
			maxDepth: 24,
			ids: []string{
				"0000000000000000000000000000000000000000000000000000000000000000",
				"123456789abcdef0000000000000000000000000000000000000000000000000",
				"5555555555555555555555555555555555555555555555555555555555555555",
				"8888888888888888888888888888888888888888888888888888888888888888",
				"abcdef1234567890000000000000000000000000000000000000000000000000",
			},
			ranges: []rangeTestCase{
				{
					xIdx:     0,
					yIdx:     0,
					limit:    -1,
					fp:       "642464b773377bbddddddddd",
					count:    5,
					itype:    0,
					startIdx: 0,
					endIdx:   0,
				},
				{
					xIdx:     0,
					yIdx:     0,
					limit:    0,
					fp:       "000000000000000000000000",
					count:    0,
					itype:    0,
					startIdx: 0,
					endIdx:   0,
				},
				{
					xIdx:     0,
					yIdx:     0,
					limit:    3,
					fp:       "4761032dcfe98ba555555555",
					count:    3,
					itype:    0,
					startIdx: 0,
					endIdx:   3,
				},
				{
					xIdx:     4,
					yIdx:     4,
					limit:    -1,
					fp:       "642464b773377bbddddddddd",
					count:    5,
					itype:    0,
					startIdx: 4,
					endIdx:   4,
				},
				{
					xIdx:     4,
					yIdx:     4,
					limit:    1,
					fp:       "abcdef123456789000000000",
					count:    1,
					itype:    0,
					startIdx: 4,
					endIdx:   0,
				},
				{
					xIdx:     0,
					yIdx:     1,
					limit:    -1,
					fp:       "000000000000000000000000",
					count:    1,
					itype:    -1,
					startIdx: 0,
					endIdx:   1,
				},
				{
					xIdx:     0,
					yIdx:     3,
					limit:    -1,
					fp:       "4761032dcfe98ba555555555",
					count:    3,
					itype:    -1,
					startIdx: 0,
					endIdx:   3,
				},
				{
					xIdx:     0,
					yIdx:     4,
					limit:    3,
					fp:       "4761032dcfe98ba555555555",
					count:    3,
					itype:    -1,
					startIdx: 0,
					endIdx:   3,
				},
				{
					xIdx:     0,
					yIdx:     4,
					limit:    0,
					fp:       "000000000000000000000000",
					count:    0,
					itype:    -1,
					startIdx: 0,
					endIdx:   0,
				},
				{
					xIdx:     1,
					yIdx:     4,
					limit:    -1,
					fp:       "cfe98ba54761032ddddddddd",
					count:    3,
					itype:    -1,
					startIdx: 1,
					endIdx:   4,
				},
				{
					xIdx:     1,
					yIdx:     0,
					limit:    -1,
					fp:       "642464b773377bbddddddddd",
					count:    4,
					itype:    1,
					startIdx: 1,
					endIdx:   0,
				},
				{
					xIdx:     2,
					yIdx:     0,
					limit:    -1,
					fp:       "761032cfe98ba54ddddddddd",
					count:    3,
					itype:    1,
					startIdx: 2,
					endIdx:   0,
				},
				{
					xIdx:     2,
					yIdx:     0,
					limit:    0,
					fp:       "000000000000000000000000",
					count:    0,
					itype:    1,
					startIdx: 2,
					endIdx:   2,
				},
				{
					xIdx:     3,
					yIdx:     1,
					limit:    -1,
					fp:       "2345679abcdef01888888888",
					count:    3,
					itype:    1,
					startIdx: 3,
					endIdx:   1,
				},
				{
					xIdx:     3,
					yIdx:     2,
					limit:    -1,
					fp:       "317131e226622ee888888888",
					count:    4,
					itype:    1,
					startIdx: 3,
					endIdx:   2,
				},
				{
					xIdx:     3,
					yIdx:     2,
					limit:    3,
					fp:       "2345679abcdef01888888888",
					count:    3,
					itype:    1,
					startIdx: 3,
					endIdx:   1,
				},
				{
					x:        "fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff0",
					y:        "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
					limit:    -1,
					fp:       "000000000000000000000000",
					count:    0,
					itype:    -1,
					startIdx: 0,
					endIdx:   0,
				},
			},
		},
		{
			name:     "ids2",
			maxDepth: 24,
			ids: []string{
				"6e476ca729c3840d0118785496e488124ee7dade1aef0c87c6edc78f72e4904f",
				"829977b444c8408dcddc1210536f3b3bdc7fd97777426264b9ac8f70b97a7fd1",
				"a280bcb8123393e0d4a15e5c9850aab5dddffa03d5efa92e59bc96202e8992bc",
				"e93163f908630280c2a8bffd9930aa684be7a3085432035f5c641b0786590d1d",
			},
			ranges: []rangeTestCase{
				{
					xIdx:     0,
					yIdx:     0,
					limit:    -1,
					fp:       "a76fc452775b55e0dacd8be5",
					count:    4,
					itype:    0,
					startIdx: 0,
					endIdx:   0,
				},
				{
					xIdx:     0,
					yIdx:     0,
					limit:    3,
					fp:       "4e5ea7ab7f38576018653418",
					count:    3,
					itype:    0,
					startIdx: 0,
					endIdx:   3,
				},
				{
					xIdx:     0,
					yIdx:     3,
					limit:    -1,
					fp:       "4e5ea7ab7f38576018653418",
					count:    3,
					itype:    -1,
					startIdx: 0,
					endIdx:   3,
				},
				{
					xIdx:     3,
					yIdx:     1,
					limit:    -1,
					fp:       "87760f5e21a0868dc3b0c7a9",
					count:    2,
					itype:    1,
					startIdx: 3,
					endIdx:   1,
				},
				{
					xIdx:     3,
					yIdx:     2,
					limit:    -1,
					fp:       "05ef78ea6568c6000e6cd5b9",
					count:    3,
					itype:    1,
					startIdx: 3,
					endIdx:   2,
				},
			},
		},
		{
			name:     "ids3",
			maxDepth: 4,
			ids: []string{
				"01dd08ec0c477312f0ef010789b4a7c65d664e3d07e9fde246c70ee2af71f4c7",
				"051f49b4621dad18ab3582eeeda995bba5fdd0a23d0ae0387e312e4706c62d26",
				"0743ede445d407d164e4139c440e6f09273d6ac088f929c5781ffd6c63806622",
				"114991f28f34d1239d9b617ad1d0e3497fd8f7c5320c1bfc51042cddb3c4d4d1",
				"120bf12c57659760f1b0a5cf5f85e23492f92822e714543fc4be732d4de3d284",
				"20e8cb9ba6fba6926ed5e0101e57881094d831a9b26a68d73b04d30a2100075b",
				"2403eb652598ee893b84d854f222fc0231ee1c3823bba9dfbe7bc8521eb10831",
				"282ed276fe896730d856ca373837ef6f89b2109d04a0b17eac152df73fc21d90",
				"2e6690d307c831a1e87039fcb67a0cdd44867271a8955b8003e74f4c644bd7bd",
				"360ca30d3013940704a5a095318e022ee5d36618c4ad1b2d084e2bc797a1793d",
				"3f52547180ba19ae700cb24b220fac01159c489e4ab127ee7ae046069165587a",
				"4df3f9fb5b1cc7a7921dbdaf27afd16f1749f4134d611eead0a1e9cf34c51994",
				"625df1cf9e472cd647b3e5fd065be537385889b1b913a0336787a37f12d55a02",
				"6feaf52c2f8030e3eb21935f67d6ced8b37535387a086d46de8f31e5b67e1f71",
				"75a5176eb4cc182302120e991f88cbe3b01e19a28dfd972a441a5bcde57f6879",
				"768281853be35aa50156598308f6c5b12a4457615551c688712607069517714f",
				"7686323c12f0853555450ce1ec22700861530fa67d523587bf7078f915204cc5",
				"a6df4f61a0e351bc539b32b4262446ac27766073515ef4b5203941fef7343ebc",
				"a740ea1cdb1c144da5bc4f96833a4c611fa7196d4ebaa89a1bd209abe519503a",
				"ab0960667a9bf57138c1a3f7d54b242e23b6c36fd8f2a645ed9217050dd5e011",
				"af5adcf404035e9ee88377230d26406702259ad25a04d425bd3c2cff546d32c0",
				"afd06a52970126024887099ed40d2400b9bb9505f171fb203baf74f7199f7c7e",
				"b520c3bb04061813e57d75db0a06f711b635b0aef1561d01859f122439437d61",
				"b525b9ecbf8a888a3b01669c7c7d5656b6b6a7c4df3bbe5402fbe4e718bad4bb",
				"b84d4bf077d68821ee9203aaf6eee90fe892f42faee939c974f719c29117ddb6",
				"bf0f6ef1cee0eb3131fb24ef52e6ac8f0a22d85d32c3fe3255d921037423df1b",
				"c72caa7c9822d6c77a254c12bc17eae8e5d637a929c94cc84aa4662d4baa508d",
				"d4375ae1c64c3d2167bb467acc63083851d834fa24f285d4a1220c407287cd56",
				"d552081889142b74ab0f0cb9da0de192cdd549213a2d348e0cc21061c196ed6a",
				"e1729d5eda4d6dac38070551a0956f3bcf0d8ac34b45a0b7e5553315cc662ebe",
				"e41d8c3a7607ec5423cc376a34d21494f2d0c625fb9bebcec09d06c188ab7f3f",
				"e9110a384198b47be2bb63e64f094069a0ee9a013e013176bbe8189834c5e4c8",
			},
			ranges: []rangeTestCase{
				{
					xIdx:     31,
					yIdx:     0,
					limit:    -1,
					fp:       "e9110a384198b47be2bb63e6",
					count:    1,
					itype:    1,
					startIdx: 31,
					endIdx:   0,
				},
			},
		},
		{
			name:     "ids4",
			maxDepth: 24,
			ids: []string{
				"0451cd036aff0367b07590032da827b516b63a4c1b36ea9a253dcf9a7e084980",
				"0e75d10a8e98a4307dd9d0427dc1d2ebf9e45b602d159ef62c5da95197159844",
				"18040e78f834b879a9585fba90f6f5e7394dc3bb27f20829baf6bfc9e1bfe44b",
				"1a9b743abdabe7970041ba2006c0e8bb51a27b1dbfd1a8c70ef5e7703ddeaa55",
				"1b49b5a17161995cc288523637bd63af5bed99f4f7188effb702da8a7a4beee1",
				"2023eee75bec75da61ad7644bd43f02b9397a72cf489565cb53a4337975a290b",
				"24b31a6acc8cd13b119dd5aa81a6c3803250a8a79eb32231f16b09e0971f1b23",
				"2664e267650ee22dee7d8c987b5cf44ba5596c78df3db5b99fb0ce79cc649d69",
				"33940245f4aace670c84f471ff4e862d1d82ce0ada9b98a753038b4f9e60e330",
				"366d9e7adb3932e52e0a92a0afc75a2875995e7de8e0c4159e22eb97526a3547",
				"66883aa35d2c8d293f07c5c5c40c63416317423418fe5c7fd17b5fb68b3e976e",
				"80fce3e9654459cff3441e1a96413f0872e0b6f093879609696042fcfe1c8115",
				"8b2025fbe0bbebea4baee48bac9a63a4013a2ec898d7b0a518eccdb99bdb368e",
				"8e3e609653adfddcdcb6ddda7461db3a2fc822c3f96874a002f715b80865e575",
				"9b25e39d6cc3beac3ecc12140f46a699880ac8303555c694fd40ba8e61bb8b47",
				"a3c8628a1b28d1ba6f3d8beb4a29315c02789c5b53a095fa7865c9b3041502d6",
				"a98fdcab5e351a1bfd25ddcf9973e9c56a4b688d78743a8a03fa3b1d53da4949",
				"ac9c015dd51defacfc14bd4c9c8eedb89aad884bef493553a189a2915c828e95",
				"ba745196493a8368ef091860f2692978b381f67566d3413e85167672d672c8ac",
				"c26353d8bc9a1eea8e79fd693c1a1e58dacded75ceda84ed6c356bcf02b6d0f1",
				"c3f126a37c2e33b6258c87fd043026dacf0b8dd4df7a9afd7cdc293b075e1878",
				"cefd0cc8b32929df07b6ebb5b6e433f28d5460f143814f3f651330ea15e5d6e7",
				"d9390718256e71edfe671334edbfcbed8b4de3221db55805ebf606c73fe969f1",
				"db7ee147da05a5cbec3f59b020cbdba88e40ab6b212ae93c98d5a210d83a4a7b",
				"deab906f979a647eff85f3a54e5edd665f2536e0005812aee2e5e411ae71855e",
				"e0b6ab7f483527771faadbee8b4ed99ae96167d054ae5c513faf00c78aa36bdd",
				"e4ed6f5dcf179a4f10521d58d65d423098af5f6f18c42f3125a5917d338b7477",
				"e53de3ec53ba88029a2a0459a3ab82cdb3726c8aeccabf38a04e048b9add92ef",
				"f2aff99498615c44d94266060e948c11bb275ec37d0d3c651bb3ba0039a11a64",
				"f7f81332b63b79718f0321660a5cd8f6970474ff873afcdebb0d3436a2ad12ac",
				"fb42c36089a4883bc7ceaae9a57924d78557edb63ede3d5a2cf2d1f08db799d0",
				"fe494ce48f5826c00f6bc6af74258ec6e47b92365850deed95b5bfcaeccc6be8",
			},
			ranges: []rangeTestCase{
				{
					x:        "582485793d71c3e8429b9b2c8df360c2ea7bf90080d5bf375fe4618b00f59c0b",
					y:        "7eff517d2f11ed32f935be3001499ac779160a4891a496f88da0ceb33e3496cc",
					limit:    -1,
					fp:       "66883aa35d2c8d293f07c5c5",
					count:    1,
					itype:    -1,
					startIdx: 10,
					endIdx:   11,
				},
			},
		},
		{
			name:     "ids5",
			maxDepth: 24,
			ids: []string{
				"06a1f93f0dd88b60473d73127196631134382d59b7cd9b3e6bd6b4f25dd1c782",
				"488da52a035df8674aa658d30ff58de82c9dc2ae9c474e004d585c52979eacbb",
				"b5527010e990254702f77ffc8a6d6b499040bc3dc61b169a56fbc690e970c046",
				"e10fc3141c5e3a00861a4dddb495a33736f845bff62fd295985b7dfa6bcbfc91",
			},
			ranges: []rangeTestCase{
				{
					xIdx:     2,
					yIdx:     0,
					limit:    1,
					fp:       "b5527010e990254702f77ffc",
					count:    1,
					itype:    1,
					startIdx: 2,
					endIdx:   3,
				},
			},
		},
		{
			name:     "ids6",
			maxDepth: 24,
			ids: []string{
				"2727d39a2150ef91ef09fa0b60950a189d73e53fd73c1fc7a74e0a393582e51e",
				"96a3a7cfdc9ec9101fd4a8bdf831c54053c2cd0b06a6914772edb68a0153fdec",
				"b80318c43da5e4b56aa3b7f408a8f86c98418e5b364ef67a37db6017097c2ebc",
				"b899092149e332f9686e02e2878e63b7ac85694eeadfe02c94f4f15627f41bcc",
			},
			ranges: []rangeTestCase{
				{
					xIdx:     3,
					yIdx:     3,
					limit:    2,
					fp:       "9fbedabb68b3dd688767f8e9",
					count:    2,
					itype:    0,
					startIdx: 3,
					endIdx:   1,
				},
			},
		},
		{
			name:     "ids7",
			maxDepth: 24,
			ids: []string{
				"3595ec355452c94143c6bdae281b162e5b0997e6392dd1a345146861b8fb4586",
				"68d02e8f0c69b0b16dc73dda147a231a09b32d709b9b4028f13ee7ffa2e820c8",
				"7079bb2d00f961b4dc42911e2009411ceb7b8c950492a627111b60773a31c2ce",
				"ad69fbf959a0b0ba1042a2b13d1b2c9a17f8507c642e55dd93277fe8dab378a6",
			},
			ranges: []rangeTestCase{
				{
					x:     "4844a20cd5a83c101cc522fa37539412d0aac4c76a48b940e1845c3f2fe79c85",
					y:     "cb93566c2037bc8353162e9988974e4585c14f656bf6aed8fa51d00e1ae594de",
					limit: -1,
					// fingerprint: 0xb5, 0xc0, 0x6e, 0x5b, 0x55, 0x30, 0x61, 0xbf, 0xa1, 0xc7, 0xe, 0x75
					fp:       "b5c06e5b553061bfa1c70e75",
					count:    3,
					itype:    -1,
					startIdx: 1,
					endIdx:   0,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var np nodePool
			idStore := makeIDStore(tc.maxDepth)
			ft := newFPTree(&np, idStore, 32, tc.maxDepth)
			// ft.traceEnabled = true
			var hs []types.Hash32
			for _, hex := range tc.ids {
				h := types.HexToHash32(hex)
				hs = append(hs, h)
				ft.addHash(h[:])
			}

			var sb strings.Builder
			ft.dump(&sb)
			t.Logf("tree:\n%s", sb.String())

			checkTree(t, ft, tc.maxDepth)

			for _, rtc := range tc.ranges {
				var x, y types.Hash32
				var name string
				if rtc.x != "" {
					x = types.HexToHash32(rtc.x)
					y = types.HexToHash32(rtc.y)
					name = fmt.Sprintf("%s-%s_%d", rtc.x, rtc.y, rtc.limit)
				} else {
					x = hs[rtc.xIdx]
					y = hs[rtc.yIdx]
					name = fmt.Sprintf("%d-%d_%d", rtc.xIdx, rtc.yIdx, rtc.limit)
				}
				t.Run(name, func(t *testing.T) {
					fpr, err := ft.fingerprintInterval(x[:], y[:], rtc.limit)
					require.NoError(t, err)
					assert.Equal(t, rtc.fp, fpr.fp.String(), "fp")
					assert.Equal(t, rtc.count, fpr.count, "count")
					assert.Equal(t, rtc.itype, fpr.itype, "itype")

					if rtc.startIdx == -1 {
						require.Nil(t, fpr.start, "start")
					} else {
						require.NotNil(t, fpr.start, "start")
						expK := KeyBytes(hs[rtc.startIdx][:])
						assert.Equal(t, expK, fpr.start.Key(), "start")
					}

					if rtc.endIdx == -1 {
						require.Nil(t, fpr.end, "end")
					} else {
						require.NotNil(t, fpr.end, "end")
						expK := KeyBytes(hs[rtc.endIdx][:])
						assert.Equal(t, expK, fpr.end.Key(), "end")
					}
				})
			}

			ft.release()
			require.Zero(t, np.count())
		})
	}
}

func TestFPTree(t *testing.T) {
	t.Run("in-memory id store", func(t *testing.T) {
		testFPTree(t, func(maxDepth int) idStore {
			return newInMemIDStore(32)
		})
	})
	t.Run("fake ATX store", func(t *testing.T) {
		db := populateDB(t, 32, nil)
		testFPTree(t, func(maxDepth int) idStore {
			_, err := db.Exec("delete from foo", nil, nil)
			require.NoError(t, err)
			return newFakeATXIDStore(db, maxDepth)
		})
	})
}

type noIDStore struct{}

var _ idStore = noIDStore{}

func (noIDStore) clone() idStore {
	return &noIDStore{}
}

func (noIDStore) registerHash(h KeyBytes) error {
	return nil
}

func (noIDStore) start() (iterator, error) {
	panic("no ID store")

}

func (noIDStore) iter(from KeyBytes) (iterator, error) {
	return noIter{}, nil
}

type noIter struct{}

func (noIter) Key() hashsync.Ordered {
	return make(KeyBytes, 32)
}

func (noIter) Next() error {
	panic("no ID store")
}

func (noIter) clone() iterator {
	return noIter{}
}

var _ iterator = &noIter{}

// TestFPTreeNoIDStore tests that an fpTree can avoid using an idStore if X has only
// 0 bits below max-depth and Y has only 1 bits below max-depth. It also checks that an fpTree
// can avoid using an idStore in "relaxed count" mode for splitting ranges.
func TestFPTreeNoIDStore(t *testing.T) {
	var np nodePool
	ft := newFPTree(&np, &noIDStore{}, 32, 24)
	// ft.traceEnabled = true
	hashes := []KeyBytes{
		util.FromHex("1111111111111111111111111111111111111111111111111111111111111111"),
		util.FromHex("2222222222222222222222222222222222222222222222222222222222222222"),
		util.FromHex("4444444444444444444444444444444444444444444444444444444444444444"),
		util.FromHex("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	for _, h := range hashes {
		ft.addHash(h)
	}

	for _, tc := range []struct {
		x, y  KeyBytes
		limit int
		fp    string
		count uint32
	}{
		{
			x:     hashes[0],
			y:     hashes[0],
			limit: -1,
			fp:    "ffffffffffffffffffffffff",
			count: 4,
		},
		{
			x:     util.FromHex("1111110000000000000000000000000000000000000000000000000000000000"),
			y:     util.FromHex("1111120000000000000000000000000000000000000000000000000000000000"),
			limit: -1,
			fp:    "111111111111111111111111",
			count: 1,
		},
		{
			x:     util.FromHex("0000000000000000000000000000000000000000000000000000000000000000"),
			y:     util.FromHex("9000000000000000000000000000000000000000000000000000000000000000"),
			limit: -1,
			fp:    "ffffffffffffffffffffffff",
			count: 4,
		},
	} {
		fpr, err := ft.fingerprintInterval(tc.x, tc.y, tc.limit)
		require.NoError(t, err)
		require.Equal(t, tc.fp, fpr.fp.String(), "fp")
		require.Equal(t, tc.count, fpr.count, "count")
	}
}

func TestFPTreeClone(t *testing.T) {
	var np nodePool
	ft1 := newFPTree(&np, newInMemIDStore(32), 32, 24)
	hashes := []types.Hash32{
		types.HexToHash32("1111111111111111111111111111111111111111111111111111111111111111"),
		types.HexToHash32("3333333333333333333333333333333333333333333333333333333333333333"),
		types.HexToHash32("4444444444444444444444444444444444444444444444444444444444444444"),
	}
	ft1.addHash(hashes[0][:])
	ft1.addHash(hashes[1][:])

	fpr, err := ft1.fingerprintInterval(hashes[0][:], hashes[0][:], -1)
	require.NoError(t, err)
	require.Equal(t, "222222222222222222222222", fpr.fp.String(), "fp")
	require.Equal(t, uint32(2), fpr.count, "count")
	require.Equal(t, 0, fpr.itype, "itype")

	var sb strings.Builder
	ft1.dump(&sb)
	t.Logf("ft1 pre-clone:\n%s", sb.String())

	ft2 := ft1.clone()

	sb.Reset()
	ft1.dump(&sb)
	t.Logf("ft1 after-clone:\n%s", sb.String())

	sb.Reset()
	ft2.dump(&sb)
	t.Logf("ft2 after-clone:\n%s", sb.String())

	// original tree unchanged --- rmme!!!!
	fpr, err = ft1.fingerprintInterval(hashes[0][:], hashes[0][:], -1)
	require.NoError(t, err)
	require.Equal(t, "222222222222222222222222", fpr.fp.String(), "fp")
	require.Equal(t, uint32(2), fpr.count, "count")
	require.Equal(t, 0, fpr.itype, "itype")

	ft2.addHash(hashes[2][:])

	fpr, err = ft2.fingerprintInterval(hashes[0][:], hashes[0][:], -1)
	require.NoError(t, err)
	require.Equal(t, "666666666666666666666666", fpr.fp.String(), "fp")
	require.Equal(t, uint32(3), fpr.count, "count")
	require.Equal(t, 0, fpr.itype, "itype")

	// original tree unchanged
	fpr, err = ft1.fingerprintInterval(hashes[0][:], hashes[0][:], -1)
	require.NoError(t, err)
	require.Equal(t, "222222222222222222222222", fpr.fp.String(), "fp")
	require.Equal(t, uint32(2), fpr.count, "count")
	require.Equal(t, 0, fpr.itype, "itype")

	sb.Reset()
	ft1.dump(&sb)
	t.Logf("ft1:\n%s", sb.String())

	sb.Reset()
	ft2.dump(&sb)
	t.Logf("ft2:\n%s", sb.String())

	ft1.release()
	ft2.release()

	require.Zero(t, np.count())
}

type hashList []types.Hash32

func (l hashList) findGTE(h types.Hash32) int {
	p, _ := slices.BinarySearchFunc(l, h, func(a, b types.Hash32) int {
		return a.Compare(b)
	})
	return p
}

func (l hashList) keyAt(p int) KeyBytes {
	if p == len(l) {
		p = 0
	}
	return KeyBytes(l[p][:])
}

func checkNode(t *testing.T, ft *fpTree, idx nodeIndex, depth int) {
	node := ft.np.node(idx)
	if node.left == noIndex && node.right == noIndex {
		if node.c != 1 {
			require.Equal(t, depth, ft.maxDepth)
		}
	} else {
		require.Less(t, depth, ft.maxDepth)
		var expFP fingerprint
		var expCount uint32
		if node.left != noIndex {
			checkNode(t, ft, node.left, depth+1)
			left := ft.np.node(node.left)
			expFP.update(left.fp[:])
			expCount += left.c
		}
		if node.right != noIndex {
			checkNode(t, ft, node.right, depth+1)
			right := ft.np.node(node.right)
			expFP.update(right.fp[:])
			expCount += right.c
		}
		require.Equal(t, expFP, node.fp, "node fp at depth %d", depth)
		require.Equal(t, expCount, node.c, "node count at depth %d", depth)
	}
}

func checkTree(t *testing.T, ft *fpTree, maxDepth int) {
	require.Equal(t, maxDepth, ft.maxDepth)
	if ft.root != noIndex {
		checkNode(t, ft, ft.root, 0)
	}
}

func repeatTestFPTreeManyItems(
	t *testing.T,
	makeIDStore idStoreFunc,
	randomXY bool,
	numItems, maxDepth int,
	repeatOuter, repeatInner int,
) {
	for i := 0; i < repeatOuter; i++ {
		testFPTreeManyItems(t, makeIDStore(maxDepth), randomXY, numItems, maxDepth, repeatInner)
	}
}

type fpResultWithBounds struct {
	fp    fingerprint
	count uint32
	itype int
	start KeyBytes
	end   KeyBytes
}

func toFPResultWithBounds(fpr fpResult) fpResultWithBounds {
	r := fpResultWithBounds{
		fp:    fpr.fp,
		count: fpr.count,
		itype: fpr.itype,
	}
	if fpr.start != nil {
		r.start = fpr.start.Key().(KeyBytes)
	}
	if fpr.end != nil {
		r.end = fpr.end.Key().(KeyBytes)
	}
	return r
}

func dumbFP(hs hashList, x, y types.Hash32, limit int) fpResultWithBounds {
	var fpr fpResultWithBounds
	l := len(hs)
	if l == 0 {
		return fpr
	}
	fpr.itype = x.Compare(y)
	switch fpr.itype {
	case -1:
		p := hs.findGTE(x)
		pY := hs.findGTE(y)
		// t.Logf("x=%s y=%s pX=%d y=%d", x.String(), y.String(), pX, pY)
		fpr.start = hs.keyAt(p)
		for {
			if p >= pY || limit == 0 {
				fpr.end = hs.keyAt(p)
				break
			}
			// t.Logf("XOR %s", hs[p].String())
			fpr.fp.update(hs.keyAt(p))
			limit--
			fpr.count++
			p++
		}
	case 1:
		p := hs.findGTE(x)
		fpr.start = hs.keyAt(p)
		for {
			if p >= len(hs) || limit == 0 {
				fpr.end = hs.keyAt(p)
				break
			}
			fpr.fp.update(hs.keyAt(p))
			limit--
			fpr.count++
			p++
		}
		if limit == 0 {
			return fpr
		}
		pY := hs.findGTE(y)
		p = 0
		for {
			if p == pY || limit == 0 {
				fpr.end = hs.keyAt(p)
				break
			}
			fpr.fp.update(hs.keyAt(p))
			limit--
			fpr.count++
			p++
		}
	default:
		pX := hs.findGTE(x)
		p := pX
		fpr.start = hs.keyAt(p)
		fpr.end = fpr.start
		for {
			if limit == 0 {
				fpr.end = hs.keyAt(p)
				break
			}
			fpr.fp.update(hs.keyAt(p))
			limit--
			fpr.count++
			p = (p + 1) % l
			if p == pX {
				break
			}
		}
	}
	return fpr
}

func verifyInterval(t *testing.T, hs hashList, ft *fpTree, x, y types.Hash32, limit int) fpResult {
	expFPR := dumbFP(hs, x, y, limit)
	fpr, err := ft.fingerprintInterval(x[:], y[:], limit)
	require.NoError(t, err)
	require.Equal(t, expFPR, toFPResultWithBounds(fpr),
		"x=%s y=%s limit=%d", x.String(), y.String(), limit)

	// QQQQQ: rm
	if !reflect.DeepEqual(toFPResultWithBounds(fpr), expFPR) {
		t.Logf("QQQQQ: x=%s y=%s", x.String(), y.String())
		for _, h := range hs {
			t.Logf("QQQQQ: hash: %s", h.String())
		}
		var sb strings.Builder
		ft.dump(&sb)
		t.Logf("QQQQQ: tree:\n%s", sb.String())
	}
	// QQQQQ: /rm

	require.Equal(t, expFPR, toFPResultWithBounds(fpr),
		"x=%s y=%s limit=%d", x.String(), y.String(), limit)

	return fpr
}

func verifySubIntervals(t *testing.T, hs hashList, ft *fpTree, x, y types.Hash32, limit, d int) fpResult {
	fpr := verifyInterval(t, hs, ft, x, y, limit)
	// t.Logf("verifySubIntervals: x=%s y=%s limit=%d => count %d", x.String(), y.String(), limit, fpr.count)
	if fpr.count > 1 {
		c := int((fpr.count + 1) / 2)
		if limit >= 0 {
			require.Less(t, c, limit)
		}
		part := verifyInterval(t, hs, ft, x, y, c)
		var m types.Hash32
		copy(m[:], part.end.Key().(KeyBytes))
		verifySubIntervals(t, hs, ft, x, m, -1, d+1)
		verifySubIntervals(t, hs, ft, m, y, -1, d+1)
	}
	return fpr
}

func testFPTreeManyItems(t *testing.T, idStore idStore, randomXY bool, numItems, maxDepth, repeat int) {
	var np nodePool
	ft := newFPTree(&np, idStore, 32, maxDepth)
	// ft.traceEnabled = true
	hs := make(hashList, numItems)
	var fp fingerprint
	for i := range hs {
		h := types.RandomHash()
		hs[i] = h
		ft.addHash(h[:])
		fp.update(h[:])
	}
	slices.SortFunc(hs, func(a, b types.Hash32) int {
		return a.Compare(b)
	})

	checkTree(t, ft, maxDepth)

	fpr, err := ft.fingerprintInterval(hs[0][:], hs[0][:], -1)
	require.NoError(t, err)
	require.Equal(t, fp, fpr.fp, "fp")
	require.Equal(t, uint32(numItems), fpr.count, "count")
	require.Equal(t, 0, fpr.itype, "itype")
	for i := 0; i < repeat; i++ {
		// TBD: allow reverse order
		var x, y types.Hash32
		if randomXY {
			x = types.RandomHash()
			y = types.RandomHash()
		} else {
			x = hs[rand.Intn(numItems)]
			y = hs[rand.Intn(numItems)]
		}
		verifySubIntervals(t, hs, ft, x, y, -1, 0)
	}
}

func TestFPTreeManyItems(t *testing.T) {
	const (
		repeatOuter = 3
		repeatInner = 5
		numItems    = 1 << 10
		maxDepth    = 12
		// numItems = 1 << 5
		// maxDepth = 4
	)
	t.Run("bounds from the set", func(t *testing.T) {
		t.Parallel()
		repeatTestFPTreeManyItems(t, func(maxDepth int) idStore {
			return newInMemIDStore(32)
		}, false, numItems, maxDepth, repeatOuter, repeatInner)

	})
	t.Run("random bounds", func(t *testing.T) {
		t.Parallel()
		repeatTestFPTreeManyItems(t, func(maxDepth int) idStore {
			return newInMemIDStore(32)
		}, true, numItems, maxDepth, repeatOuter, repeatInner)
	})
	t.Run("SQL, bounds from the set", func(t *testing.T) {
		t.Parallel()
		db := populateDB(t, 32, nil)
		repeatTestFPTreeManyItems(t, func(maxDepth int) idStore {
			_, err := db.Exec("delete from foo", nil, nil)
			require.NoError(t, err)
			return newFakeATXIDStore(db, maxDepth)
		}, false, numItems, maxDepth, repeatOuter, repeatInner)
	})
	t.Run("SQL, random bounds", func(t *testing.T) {
		t.Parallel()
		db := populateDB(t, 32, nil)
		repeatTestFPTreeManyItems(t, func(maxDepth int) idStore {
			_, err := db.Exec("delete from foo", nil, nil)
			require.NoError(t, err)
			return newFakeATXIDStore(db, maxDepth)
		}, true, numItems, maxDepth, repeatOuter, repeatInner)
	})
	// TBD: test limits with both random and non-random bounds
	// TBD: test start/end iterators
}

const dbFile = "/Users/ivan4th/Library/Application Support/Spacemesh/node-data/7c8cef2b/state.sql"

// func dumbAggATXs(t *testing.T, db sql.StateDatabase, x, y types.Hash32) fpResult {
// 	var fp fingerprint
// 	ts := time.Now()
// 	nRows, err := db.Exec(
// 		// BETWEEN is faster than >= and <
// 		"select id from atxs where id between ? and ? order by id",
// 		func(stmt *sql.Statement) {
// 			stmt.BindBytes(1, x[:])
// 			stmt.BindBytes(2, y[:])
// 		},
// 		func(stmt *sql.Statement) bool {
// 			var id types.Hash32
// 			stmt.ColumnBytes(0, id[:])
// 			if id != y {
// 				fp.update(id[:])
// 			}
// 			return true
// 		},
// 	)
// 	require.NoError(t, err)
// 	t.Logf("QQQQQ: %v: dumb fp between %s and %s", time.Now().Sub(ts), x.String(), y.String())
// 	return fpResult{
// 		fp:    fp,
// 		count: uint32(nRows),
// 		itype: x.Compare(y),
// 	}
// }

func treeStats(t *testing.T, ft *fpTree) {
	numNodes := 0
	numCompactable := 0
	numLeafs := 0
	numEarlyLeafs := 0
	minLeafSize := uint32(math.MaxUint32)
	maxLeafSize := uint32(0)
	totalLeafSize := uint32(0)
	var scanNode func(nodeIndex, int) bool
	scanNode = func(idx nodeIndex, depth int) bool {
		if idx == noIndex {
			return false
		}
		numNodes++
		node := ft.np.node(idx)
		if node.leaf() {
			minLeafSize = min(minLeafSize, node.c)
			maxLeafSize = max(maxLeafSize, node.c)
			totalLeafSize += node.c
			numLeafs++
			if depth < ft.maxDepth {
				numEarlyLeafs++
			}
		} else {
			haveLeft := scanNode(node.left, depth+1)
			if !scanNode(node.right, depth+1) || !haveLeft {
				numCompactable++
			}
		}
		return true
	}
	scanNode(ft.root, 0)
	avgLeafSize := float64(totalLeafSize) / float64(numLeafs)
	t.Logf("tree stats: numNodes=%d numLeafs=%d numEarlyLeafs=%d numCompactable=%d minLeafSize=%d maxLeafSize=%d avgLeafSize=%f",
		numNodes, numLeafs, numEarlyLeafs, numCompactable, minLeafSize, maxLeafSize, avgLeafSize)
}

func testATXFP(t *testing.T, maxDepth int, hs *[]types.Hash32) {
	// t.Skip("slow tmp test")
	// counts := make(map[uint64]uint64)
	// prefLens := make(map[int]int)
	// QQQQQ: TBD: reenable schema drift check
	db, err := statesql.Open("file:"+dbFile, sql.WithIgnoreSchemaDrift())
	require.NoError(t, err)
	defer db.Close()
	// _, err = db.Exec("PRAGMA cache_size = -2000000", nil, nil)
	// require.NoError(t, err)
	// var prev uint64
	// first := true
	// where epoch=23
	var np nodePool
	if *hs == nil {
		t.Logf("loading IDs")
		_, err = db.Exec("select id from atxs where epoch = 26 order by id",
			nil, func(stmt *sql.Statement) bool {
				var id types.Hash32
				stmt.ColumnBytes(0, id[:])
				*hs = append(*hs, id)
				// v := load64(id[:])
				// counts[v>>40]++
				// if first {
				// 	first = false
				// } else {
				// 	prefLens[bits.LeadingZeros64(prev^v)]++
				// }
				// prev = v
				return true
			})
		require.NoError(t, err)
	}

	// TODO: use testing.B and b.ReportAllocs()
	for i := 0; i < 3; i++ {
		runtime.GC()
		time.Sleep(100 * time.Millisecond)
	}
	var stats1 runtime.MemStats
	runtime.ReadMemStats(&stats1)
	// TODO: pass extra bind params to the SQL query
	store := newSQLIDStore(db, "select id from atxs where id >= ? and epoch = 26 order by id limit ?", 32)
	ft := newFPTree(&np, store, 32, maxDepth)
	for _, id := range *hs {
		ft.addHash(id[:])
	}
	treeStats(t, ft)

	// countFreq := make(map[uint64]int)
	// for _, c := range counts {
	// 	countFreq[c]++
	// }
	// ks := maps.Keys(countFreq)
	// slices.Sort(ks)
	// for _, c := range ks {
	// 	t.Logf("%d: %d times", c, countFreq[c])
	// }
	// pls := maps.Keys(prefLens)
	// slices.Sort(pls)
	// for _, pl := range pls {
	// 	t.Logf("pl %d: %d times", pl, prefLens[pl])
	// }

	t.Logf("benchmarking ranges")
	ts := time.Now()
	const numIter = 20000
	for n := 0; n < numIter; n++ {
		x := types.RandomHash()
		y := types.RandomHash()
		ft.fingerprintInterval(x[:], y[:], -1)
	}
	elapsed := time.Now().Sub(ts)

	for i := 0; i < 3; i++ {
		runtime.GC()
		time.Sleep(100 * time.Millisecond)
	}
	var stats2 runtime.MemStats
	runtime.ReadMemStats(&stats2)
	t.Logf("range benchmark for maxDepth %d: %v per range, %f ranges/s, heap diff %d",
		// it's important to use ft pointer here so it doesn't get freed
		// before we read the mem stats
		ft.maxDepth,
		elapsed/numIter,
		float64(numIter)/elapsed.Seconds(),
		stats2.HeapInuse-stats1.HeapInuse)

	// TODO: test incomplete ranges (with limit)
	t.Logf("testing ranges")
	for n := 0; n < 50; n++ {
		x := types.RandomHash()
		y := types.RandomHash()
		// t.Logf("QQQQQ: x=%s y=%s", x.String(), y.String())
		expFPResult := dumbFP(*hs, x, y, -1)
		//expFPResult := dumbAggATXs(t, db, x, y)
		fpr, err := ft.fingerprintInterval(x[:], y[:], -1)
		require.NoError(t, err)
		require.Equal(t, expFPResult, toFPResultWithBounds(fpr),
			"x=%s y=%s", x.String(), y.String())

		limit := 0
		if fpr.count != 0 {
			limit = rand.Intn(int(fpr.count))
		}
		// t.Logf("QQQQQ: x=%s y=%s limit=%d", x.String(), y.String(), limit)
		expFPResult = dumbFP(*hs, x, y, limit)
		fpr, err = ft.fingerprintInterval(x[:], y[:], limit)
		require.NoError(t, err)
		require.Equal(t, expFPResult, toFPResultWithBounds(fpr),
			"x=%s y=%s limit=%d", x.String(), y.String(), limit)
	}

	// x := types.HexToHash32("930a069661bf21b52aa79a4b5149ecc1190282f1386b6b8ae6b738153a7a802d")
	// y := types.HexToHash32("6c966fc65c07c92e869b7796b2346a33e01c4fe38c25094a480cdcd2e7df1f56")
	// t.Logf("QQQQQ: maxDepth=%d x=%s y=%s", maxDepth, x.String(), y.String())
	// expFPResult := dumbFP(*hs, x, y, -1)
	// //expFPResult := dumbAggATXs(t, db, x, y)
	// ft.traceEnabled = true
	// fpr, err := ft.fingerprintInterval(x[:], y[:], -1)
	// require.NoError(t, err)
	// require.Equal(t, expFPResult, fpr, "x=%s y=%s", x.String(), y.String())
}

func TestATXFP(t *testing.T) {
	t.Skip("slow test")
	var hs []types.Hash32
	for maxDepth := 15; maxDepth <= 23; maxDepth++ {
		for i := 0; i < 3; i++ {
			testATXFP(t, maxDepth, &hs)
		}
	}
}

// benchmarks

// maxDepth 18: 94.739µs per range, 10555.290991 ranges/s, heap diff 16621568
// maxDepth 18: 95.837µs per range, 10434.316922 ranges/s, heap diff 16564224
// maxDepth 18: 95.312µs per range, 10491.834238 ranges/s, heap diff 16588800
// maxDepth 19: 60.822µs per range, 16441.200726 ranges/s, heap diff 32317440
// maxDepth 19: 57.86µs per range, 17283.084675 ranges/s, heap diff 32333824
// maxDepth 19: 58.183µs per range, 17187.139809 ranges/s, heap diff 32342016
// maxDepth 20: 41.582µs per range, 24048.516680 ranges/s, heap diff 63094784
// maxDepth 20: 41.384µs per range, 24163.830753 ranges/s, heap diff 63102976
// maxDepth 20: 42.003µs per range, 23807.631953 ranges/s, heap diff 63053824
// maxDepth 21: 31.996µs per range, 31253.349138 ranges/s, heap diff 123289600
// maxDepth 21: 31.926µs per range, 31321.766830 ranges/s, heap diff 123256832
// maxDepth 21: 31.839µs per range, 31407.657854 ranges/s, heap diff 123256832
// maxDepth 22: 27.829µs per range, 35933.122150 ranges/s, heap diff 240689152
// maxDepth 22: 27.524µs per range, 36330.976995 ranges/s, heap diff 240689152
// maxDepth 22: 27.386µs per range, 36514.410406 ranges/s, heap diff 240689152
// maxDepth 23: 24.378µs per range, 41020.262869 ranges/s, heap diff 470024192
// maxDepth 23: 24.605µs per range, 40641.096389 ranges/s, heap diff 470056960
// maxDepth 23: 24.51µs per range, 40799.444720 ranges/s, heap diff 470040576

// maxDepth 18: 94.518µs per range, 10579.885738 ranges/s, heap diff 16621568
// maxDepth 18: 95.144µs per range, 10510.332936 ranges/s, heap diff 16572416
// maxDepth 18: 94.55µs per range, 10576.359829 ranges/s, heap diff 16588800
// maxDepth 19: 60.463µs per range, 16538.974879 ranges/s, heap diff 32325632
// maxDepth 19: 60.47µs per range, 16537.108181 ranges/s, heap diff 32358400
// maxDepth 19: 60.441µs per range, 16544.939001 ranges/s, heap diff 32333824
// maxDepth 20: 41.131µs per range, 24311.982297 ranges/s, heap diff 63078400
// maxDepth 20: 41.621µs per range, 24026.119996 ranges/s, heap diff 63086592
// maxDepth 20: 41.568µs per range, 24056.912641 ranges/s, heap diff 63094784
// maxDepth 21: 32.234µs per range, 31022.459566 ranges/s, heap diff 123256832
// maxDepth 21: 30.856µs per range, 32408.240119 ranges/s, heap diff 123248640
// maxDepth 21: 30.774µs per range, 32494.318758 ranges/s, heap diff 123224064
// maxDepth 22: 27.476µs per range, 36394.375781 ranges/s, heap diff 240689152
// maxDepth 22: 27.707µs per range, 36091.188900 ranges/s, heap diff 240705536
// maxDepth 22: 27.281µs per range, 36654.794863 ranges/s, heap diff 240705536
// maxDepth 23: 24.394µs per range, 40992.220132 ranges/s, heap diff 470048768
// maxDepth 23: 24.697µs per range, 40489.695824 ranges/s, heap diff 470040576
// maxDepth 23: 24.436µs per range, 40923.081488 ranges/s, heap diff 470032384

// maxDepth 15: 529.513µs per range, 1888.524885 ranges/s, heap diff 2293760
// maxDepth 15: 528.783µs per range, 1891.132520 ranges/s, heap diff 2244608
// maxDepth 15: 529.458µs per range, 1888.723450 ranges/s, heap diff 2252800
// maxDepth 16: 281.809µs per range, 3548.498801 ranges/s, heap diff 4390912
// maxDepth 16: 280.159µs per range, 3569.389929 ranges/s, heap diff 4382720
// maxDepth 16: 280.449µs per range, 3565.709031 ranges/s, heap diff 4390912
// maxDepth 17: 157.429µs per range, 6352.037713 ranges/s, heap diff 8527872
// maxDepth 17: 156.569µs per range, 6386.942961 ranges/s, heap diff 8527872
// maxDepth 17: 157.158µs per range, 6362.998907 ranges/s, heap diff 8527872
// maxDepth 18: 94.689µs per range, 10560.886016 ranges/s, heap diff 16547840
// maxDepth 18: 95.995µs per range, 10417.191145 ranges/s, heap diff 16564224
// maxDepth 18: 94.469µs per range, 10585.428908 ranges/s, heap diff 16515072
// maxDepth 19: 61.218µs per range, 16334.822475 ranges/s, heap diff 32342016
// maxDepth 19: 61.733µs per range, 16198.549404 ranges/s, heap diff 32350208
// maxDepth 19: 61.269µs per range, 16321.226214 ranges/s, heap diff 32309248
// maxDepth 20: 42.336µs per range, 23620.054892 ranges/s, heap diff 63053824
// maxDepth 20: 41.906µs per range, 23862.511368 ranges/s, heap diff 63094784
// maxDepth 20: 41.647µs per range, 24011.273302 ranges/s, heap diff 63086592
// maxDepth 21: 32.895µs per range, 30399.444906 ranges/s, heap diff 123256832
// maxDepth 21: 31.798µs per range, 31447.748207 ranges/s, heap diff 123256832
// maxDepth 21: 32.008µs per range, 31241.248008 ranges/s, heap diff 123265024
// maxDepth 22: 27.014µs per range, 37017.223157 ranges/s, heap diff 240689152
// maxDepth 22: 26.764µs per range, 37363.422097 ranges/s, heap diff 240664576
// maxDepth 22: 26.938µs per range, 37121.580267 ranges/s, heap diff 240664576
// maxDepth 23: 24.457µs per range, 40887.173321 ranges/s, heap diff 470040576
// maxDepth 23: 24.997µs per range, 40003.930386 ranges/s, heap diff 470040576
// maxDepth 23: 24.741µs per range, 40418.462446 ranges/s, heap diff 470040576

// TODO: QQQQQ: retrieve the end of the interval w/count in fpTree.fingerprintInterval()
// TODO: QQQQQ: test limits in TestInMemFPTreeManyItems (sep test cases SQL / non-SQL)
// TODO: the returned RangeInfo.End iterators should be cyclic

// TBD: random off-by-1 failure?
//             	--- Expected
//             	+++ Actual
//             	@@ -2,5 +2,5 @@
//             	  fp: (dbsync.fingerprint) (len=12) {
//             	-  00000000  30 d4 db 9d b9 15 dd ad  75 1e 67 fd              |0.......u.g.|
//             	+  00000000  a3 de 4b 89 7b 93 fc 76  24 88 82 b2              |..K.{..v$...|
//             	  },
//             	- count: (uint32) 41784134,
//             	+ count: (uint32) 41784135,
//             	  itype: (int) 1
// Test:       	TestATXFP
// Messages:   	x=930a069661bf21b52aa79a4b5149ecc1190282f1386b6b8ae6b738153a7a802d y=6c966fc65c07c92e869b7796b2346a33e01c4fe38c25094a480cdcd2e7df1f56
