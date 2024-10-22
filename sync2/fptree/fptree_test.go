package fptree_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/fptree"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
)

const (
	testKeyLen = 32
	testDepth  = 24
)

func requireEmpty(t *testing.T, sr rangesync.SeqResult) {
	for range sr.Seq {
		require.Fail(t, "expected an empty sequence")
	}
	require.NoError(t, sr.Error())
}

func firstKey(t *testing.T, sr rangesync.SeqResult) rangesync.KeyBytes {
	k, err := sr.First()
	require.NoError(t, err)
	return k
}

func testFPTree(t *testing.T, makeFPTrees mkFPTreesFunc) {
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
		name   string
		ids    []string
		ranges []rangeTestCase
		x, y   string
	}{
		{
			name: "empty",
			ids:  nil,
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
			name: "ids1",
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
			name: "ids2",
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
			name: "ids3",
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
			name: "ids4",
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
			name: "ids6",
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
			name: "ids7",
			ids: []string{
				"3595ec355452c94143c6bdae281b162e5b0997e6392dd1a345146861b8fb4586",
				"68d02e8f0c69b0b16dc73dda147a231a09b32d709b9b4028f13ee7ffa2e820c8",
				"7079bb2d00f961b4dc42911e2009411ceb7b8c950492a627111b60773a31c2ce",
				"ad69fbf959a0b0ba1042a2b13d1b2c9a17f8507c642e55dd93277fe8dab378a6",
			},
			ranges: []rangeTestCase{
				{
					x:        "4844a20cd5a83c101cc522fa37539412d0aac4c76a48b940e1845c3f2fe79c85",
					y:        "cb93566c2037bc8353162e9988974e4585c14f656bf6aed8fa51d00e1ae594de",
					limit:    -1,
					fp:       "b5c06e5b553061bfa1c70e75",
					count:    3,
					itype:    -1,
					startIdx: 1,
					endIdx:   0,
				},
			},
		},
		{
			name: "ids8",
			ids: []string{
				"0e69888877324da35693decc7ded1b2bac16d394ced869af494568d66473a6f0",
				"3a78db9e386493402561d9c6f69a6b434a62388f61d06d960598ebf29a3a2187",
				"66c9aa8f3be7da713db66e56cc165a46764f88d3113244dd5964bb0a10ccacc3",
				"90b25f2d1ee9c9e2d20df5f2226d14ee4223ea27ba565a49aa66a9c44a51c241",
				"9e11fdb099f1118144738f9b68ca601e74b97280fd7bbc97cfc377f432e9b7b5",
				"c1690e47798295cca02392cbfc0a86cb5204878c04a29b3ae7701b6b51681128",
			},
			ranges: []rangeTestCase{
				{
					x:        "9e11fdb099f1118144738f9b68ca601e74b97280fd7bbc97cfc377f432e9b7b5",
					y:        "0e69880000000000000000000000000000000000000000000000000000000000",
					limit:    -1,
					fp:       "5f78f3f7e073844de4501d50",
					count:    2,
					itype:    1,
					startIdx: 4,
					endIdx:   0,
				},
			},
		},
		{
			name: "ids9",
			ids: []string{
				"03744b955a21408f78eb4c1e51f897ed90c22cf561b8ecef25a4b6ec68f3e895",
				"691a62eb05d21ee9407fd48d252b5e80a525fd017e941fba383ceabe2ce0c0ee",
				"73e10ac8b36bc20195c5d1b162d05402eaef6622accf648399cb60874ac22165",
				"845c0a945137ed6b52fbb96a57909869cf34f41100a3a60e5d385d28c42621e1",
				"bc1ffc4d9fddbd9f3cd17c0fe53c6b86a2e36256f37e1e73c11e4c9effa911bf",
			},
			ranges: []rangeTestCase{
				{
					x:        "1a4f33388cab82533de99d9370fe367f654c76cd7e71a28334d993a31aa3e87a",
					y:        "6c5fe0023abc90d0a9327083ebc73c442cec8854f99e378551b502448f2ce000",
					limit:    -1,
					fp:       "691a62eb05d21ee9407fd48d",
					count:    1,
					itype:    -1,
					startIdx: 1,
					endIdx:   2,
				},
			},
		},
		{
			name: "ids10",
			ids: []string{
				"0aea5e19b9f53af915110ba1e05494666e8a1f4bb597d6ca0193c34b525f3480",
				"219d9f504af986492356061a68cd2355fd423768c70e511cd7802cd4fdbde1c5",
				"277a6bbc173628948456cbeb90309ae70ab837296f504640b53a891a3ddefb65",
				"2ff6f89a1f0655255a74ff0dc4eda3a67ff69bc9667261763536917db15d9fe2",
				"46b9e5fb278225f28885717512a4b2e5fbbc79b61bde8417cc2e5caf0ad86b17",
				"a732516bf7198a3c3cb4edc1c3b1ec11a2545844c45464df44e31135ad84fee0",
				"ea238facb9e3b3b6b9ca66bd9472b505e982ed937b22eb127269723124bb9ce8",
				"ff90f791d2678d09d12f1a672de85c5127ef1f8a47ae5e8f3b61de06fd803db7",
			},
			ranges: []rangeTestCase{
				{
					x:        "64015400af6cc54ce62fe1b478b38abfef5ab609182d6df0fd46f16c880263b2",
					y:        "0fcc4ed4c932e1f6ba53418a0116d20ab119c1152644abe5ee1ab30599cd3780",
					limit:    -1,
					fp:       "b86b774f25688e7a41409aba",
					count:    4,
					itype:    1,
					startIdx: 5,
					endIdx:   1,
				},
			},
		},
		{
			name: "ids11",
			ids: []string{
				"05ce2ac65bf22e2d196814d881125ce5e4f93078ab357e151c7bfccd9ef24f1d",
				"81f9f4becc8f91f1c37075ec810828b13d4e8d98b8207c467537043a1bb5d72c",
				"a15ecd17ec6674a14faf67649e0058366bf852bd51a0c41c15542861eaf55bac",
				"baeaf7d94cc800d38215396e46ba9e1293107a7e5c5d1cd5771f341e570b9f95",
				"bd666290c1e339e8cc9d4d1aaf3ce68169dfffbfbe112e22818c72eb373160fd",
				"d598253954cbf6719829dd4dca89106622cfb87666991214fece997855478a1c",
				"d9e7a5bfa187a248e894e5e72874b3bf40b0863f707c72ae70e2042ba497d3ec",
				"e58ededd4c54788c451ede2a3b92e62e1148fcd4184262dab28056f03b639ef5",
			},
			ranges: []rangeTestCase{
				{
					xIdx:     7,
					yIdx:     2,
					limit:    -1,
					fp:       "61b900a5db29c7509f06bf1e",
					count:    3,
					itype:    1,
					startIdx: 7,
					endIdx:   2,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trees := makeFPTrees(t)
			ft := trees[0]
			var hs []rangesync.KeyBytes
			for _, hex := range tc.ids {
				h := rangesync.MustParseHexKeyBytes(hex)
				hs = append(hs, h)
				require.NoError(t, ft.RegisterKey(h))
				fptree.AnalyzeTreeNodeRefs(t, trees...)
			}

			var sb strings.Builder
			ft.Dump(&sb)
			t.Logf("tree:\n%s", sb.String())

			fptree.CheckTree(t, ft)
			for _, k := range hs {
				require.True(t, ft.CheckKey(k), "checkKey(%s)", k.ShortString())
			}
			require.False(t, ft.CheckKey(rangesync.RandomKeyBytes(testKeyLen)), "checkKey(random)")

			for _, rtc := range tc.ranges {
				var x, y rangesync.KeyBytes
				var name string
				if rtc.x != "" {
					x = rangesync.MustParseHexKeyBytes(rtc.x)
					y = rangesync.MustParseHexKeyBytes(rtc.y)
					name = fmt.Sprintf("%s-%s_%d", rtc.x, rtc.y, rtc.limit)
				} else {
					x = hs[rtc.xIdx]
					y = hs[rtc.yIdx]
					name = fmt.Sprintf("%d-%d_%d", rtc.xIdx, rtc.yIdx, rtc.limit)
				}
				t.Run(name, func(t *testing.T) {
					fpr, err := ft.FingerprintInterval(x, y, rtc.limit)
					require.NoError(t, err)
					assert.Equal(t, rtc.fp, fpr.FP.String(), "fp")
					assert.Equal(t, rtc.count, fpr.Count, "count")
					assert.Equal(t, rtc.itype, fpr.IType, "itype")

					if rtc.startIdx == -1 {
						requireEmpty(t, fpr.Items)
					} else {
						require.NotNil(t, fpr.Items, "items")
						expK := rangesync.KeyBytes(hs[rtc.startIdx])
						assert.Equal(t, expK, firstKey(t, fpr.Items), "items")
					}

					if rtc.endIdx == -1 {
						require.Nil(t, fpr.Next, "next")
					} else {
						require.NotNil(t, fpr.Next, "next")
						expK := rangesync.KeyBytes(hs[rtc.endIdx])
						assert.Equal(t, expK, fpr.Next, "next")
					}
				})
			}

			ft.Release()
			require.Zero(t, ft.PoolNodeCount())
		})
	}
}

type mkFPTreesFunc func(t *testing.T) []*fptree.FPTree

func makeFPTreeWithValues(t *testing.T) []*fptree.FPTree {
	ft := fptree.NewFPTreeWithValues(0, testKeyLen)
	return []*fptree.FPTree{ft}
}

func makeInMemoryFPTree(t *testing.T) []*fptree.FPTree {
	store := fptree.NewFPTreeWithValues(0, testKeyLen)
	ft := fptree.NewFPTree(0, store, testKeyLen, testDepth)
	return []*fptree.FPTree{ft, store}
}

func makeDBBackedFPTree(t *testing.T) []*fptree.FPTree {
	db := sqlstore.CreateDB(t, testKeyLen)
	st := sqlstore.SyncedTable{
		TableName: "foo",
		IDColumn:  "id",
	}
	sts, err := st.Snapshot(db)
	require.NoError(t, err)
	store := fptree.NewDBBackedStore(db, sts, 0, testKeyLen)
	ft := fptree.NewFPTree(0, store, testKeyLen, testDepth)
	return []*fptree.FPTree{ft, store.FPTree}
}

func TestFPTree(t *testing.T) {
	t.Run("values in fpTree", func(t *testing.T) {
		testFPTree(t, makeFPTreeWithValues)
	})
	t.Run("in-memory fptree-based id store", func(t *testing.T) {
		testFPTree(t, makeInMemoryFPTree)
	})
	t.Run("db-backed store", func(t *testing.T) {
		testFPTree(t, makeDBBackedFPTree)
	})
}

func TestFPTreeAsStore(t *testing.T) {
	s := fptree.NewFPTreeWithValues(0, testKeyLen)

	sr := s.All()
	for range sr.Seq {
		require.Fail(t, "sequence not empty")
	}
	require.NoError(t, sr.Error())

	sr = s.From(rangesync.MustParseHexKeyBytes(
		"0000000000000000000000000000000000000000000000000000000000000000"),
		1)
	for range sr.Seq {
		require.Fail(t, "sequence not empty")
	}
	require.NoError(t, sr.Error())

	for _, h := range []string{
		"0000000000000000000000000000000000000000000000000000000000000000",
		"1234561111111111111111111111111111111111111111111111111111111111",
		"123456789abcdef0000000000000000000000000000000000000000000000000",
		"5555555555555555555555555555555555555555555555555555555555555555",
		"8888888888888888888888888888888888888888888888888888888888888888",
		"8888889999999999999999999999999999999999999999999999999999999999",
		"abcdef1234567890000000000000000000000000000000000000000000000000",
	} {
		s.RegisterKey(rangesync.MustParseHexKeyBytes(h))
	}

	sr = s.All()
	for range 3 { // make sure seq is reusable
		var r []string
		n := 15
		for k := range sr.Seq {
			r = append(r, k.String())
			n--
			if n == 0 {
				break
			}
		}
		require.NoError(t, sr.Error())
		require.Equal(t, []string{
			"0000000000000000000000000000000000000000000000000000000000000000",
			"1234561111111111111111111111111111111111111111111111111111111111",
			"123456789abcdef0000000000000000000000000000000000000000000000000",
			"5555555555555555555555555555555555555555555555555555555555555555",
			"8888888888888888888888888888888888888888888888888888888888888888",
			"8888889999999999999999999999999999999999999999999999999999999999",
			"abcdef1234567890000000000000000000000000000000000000000000000000",
			"0000000000000000000000000000000000000000000000000000000000000000",
			"1234561111111111111111111111111111111111111111111111111111111111",
			"123456789abcdef0000000000000000000000000000000000000000000000000",
			"5555555555555555555555555555555555555555555555555555555555555555",
			"8888888888888888888888888888888888888888888888888888888888888888",
			"8888889999999999999999999999999999999999999999999999999999999999",
			"abcdef1234567890000000000000000000000000000000000000000000000000",
			"0000000000000000000000000000000000000000000000000000000000000000",
		}, r)
	}

	sr = s.From(rangesync.MustParseHexKeyBytes(
		"5555555555555555555555555555555555555555555555555555555555555555"),
		1)
	for range 3 { // make sure seq is reusable
		var r []string
		n := 15
		for k := range sr.Seq {
			r = append(r, k.String())
			n--
			if n == 0 {
				break
			}
		}
		require.NoError(t, sr.Error())
		require.Equal(t, []string{
			"5555555555555555555555555555555555555555555555555555555555555555",
			"8888888888888888888888888888888888888888888888888888888888888888",
			"8888889999999999999999999999999999999999999999999999999999999999",
			"abcdef1234567890000000000000000000000000000000000000000000000000",
			"0000000000000000000000000000000000000000000000000000000000000000",
			"1234561111111111111111111111111111111111111111111111111111111111",
			"123456789abcdef0000000000000000000000000000000000000000000000000",
			"5555555555555555555555555555555555555555555555555555555555555555",
			"8888888888888888888888888888888888888888888888888888888888888888",
			"8888889999999999999999999999999999999999999999999999999999999999",
			"abcdef1234567890000000000000000000000000000000000000000000000000",
			"0000000000000000000000000000000000000000000000000000000000000000",
			"1234561111111111111111111111111111111111111111111111111111111111",
			"123456789abcdef0000000000000000000000000000000000000000000000000",
			"5555555555555555555555555555555555555555555555555555555555555555",
		}, r)
	}
}

type noIDStore struct{}

var _ sqlstore.IDStore = noIDStore{}

func (noIDStore) Clone() sqlstore.IDStore                { return &noIDStore{} }
func (noIDStore) RegisterKey(h rangesync.KeyBytes) error { return nil }
func (noIDStore) All() rangesync.SeqResult               { panic("no ID store") }
func (noIDStore) Release()                               {}

func (noIDStore) From(from rangesync.KeyBytes, sizeHint int) rangesync.SeqResult {
	return rangesync.EmptySeqResult()
}

// TestFPTreeNoIDStoreCalls tests that an fpTree can avoid using an idStore if X has only
// 0 bits below max-depth and Y has only 1 bits below max-depth. It also checks that an fpTree
// can avoid using an idStore in "relaxed count" mode for splitting ranges.
func TestFPTreeNoIDStoreCalls(t *testing.T) {
	ft := fptree.NewFPTree(0, &noIDStore{}, testKeyLen, testDepth)
	hashes := []rangesync.KeyBytes{
		rangesync.MustParseHexKeyBytes("1111111111111111111111111111111111111111111111111111111111111111"),
		rangesync.MustParseHexKeyBytes("2222222222222222222222222222222222222222222222222222222222222222"),
		rangesync.MustParseHexKeyBytes("4444444444444444444444444444444444444444444444444444444444444444"),
		rangesync.MustParseHexKeyBytes("8888888888888888888888888888888888888888888888888888888888888888"),
	}
	for _, h := range hashes {
		ft.RegisterKey(h)
	}

	for _, tc := range []struct {
		x, y  rangesync.KeyBytes
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
			x: rangesync.MustParseHexKeyBytes(
				"1111110000000000000000000000000000000000000000000000000000000000"),
			y: rangesync.MustParseHexKeyBytes(
				"1111120000000000000000000000000000000000000000000000000000000000"),
			limit: -1,
			fp:    "111111111111111111111111",
			count: 1,
		},
		{
			x: rangesync.MustParseHexKeyBytes(
				"0000000000000000000000000000000000000000000000000000000000000000"),
			y: rangesync.MustParseHexKeyBytes(
				"9000000000000000000000000000000000000000000000000000000000000000"),
			limit: -1,
			fp:    "ffffffffffffffffffffffff",
			count: 4,
		},
	} {
		fpr, err := ft.FingerprintInterval(tc.x, tc.y, tc.limit)
		require.NoError(t, err)
		require.Equal(t, tc.fp, fpr.FP.String(), "fp")
		require.Equal(t, tc.count, fpr.Count, "count")
	}
}

func TestFPTreeClone(t *testing.T) {
	store := fptree.NewFPTreeWithValues(10, testKeyLen)
	ft1 := fptree.NewFPTree(10, store, testKeyLen, testDepth)
	hashes := []rangesync.KeyBytes{
		rangesync.MustParseHexKeyBytes("1111111111111111111111111111111111111111111111111111111111111111"),
		rangesync.MustParseHexKeyBytes("3333333333333333333333333333333333333333333333333333333333333333"),
		rangesync.MustParseHexKeyBytes("4444444444444444444444444444444444444444444444444444444444444444"),
	}

	ft1.RegisterKey(hashes[0])
	fptree.AnalyzeTreeNodeRefs(t, ft1, store)

	ft1.RegisterKey(hashes[1])

	fpr, err := ft1.FingerprintInterval(hashes[0], hashes[0], -1)
	require.NoError(t, err)
	require.Equal(t, "222222222222222222222222", fpr.FP.String(), "fp")
	require.Equal(t, uint32(2), fpr.Count, "count")
	require.Equal(t, 0, fpr.IType, "itype")

	fptree.AnalyzeTreeNodeRefs(t, ft1, store)

	ft2 := ft1.Clone().(*fptree.FPTree)

	fpr, err = ft1.FingerprintInterval(hashes[0], hashes[0], -1)
	require.NoError(t, err)
	require.Equal(t, "222222222222222222222222", fpr.FP.String(), "fp")
	require.Equal(t, uint32(2), fpr.Count, "count")
	require.Equal(t, 0, fpr.IType, "itype")

	fptree.AnalyzeTreeNodeRefs(t, ft1, ft2, store, ft2.IDStore().(*fptree.FPTree))

	t.Logf("add hash to copy")
	ft2.RegisterKey(hashes[2])

	fpr, err = ft2.FingerprintInterval(hashes[0], hashes[0], -1)
	require.NoError(t, err)
	require.Equal(t, "666666666666666666666666", fpr.FP.String(), "fp")
	require.Equal(t, uint32(3), fpr.Count, "count")
	require.Equal(t, 0, fpr.IType, "itype")

	// original tree unchanged
	fpr, err = ft1.FingerprintInterval(hashes[0], hashes[0], -1)
	require.NoError(t, err)
	require.Equal(t, "222222222222222222222222", fpr.FP.String(), "fp")
	require.Equal(t, uint32(2), fpr.Count, "count")
	require.Equal(t, 0, fpr.IType, "itype")

	fptree.AnalyzeTreeNodeRefs(t, ft1, ft2, store, ft2.IDStore().(*fptree.FPTree))

	ft1.Release()
	ft2.Release()
	fptree.AnalyzeTreeNodeRefs(t, ft1, ft2, store, ft2.IDStore().(*fptree.FPTree))

	require.Zero(t, ft1.PoolNodeCount())
	require.Zero(t, ft2.PoolNodeCount())
}

func TestRandomClone(t *testing.T) {
	trees := []*fptree.FPTree{
		fptree.NewFPTree(1000, fptree.NewFPTreeWithValues(1000, testKeyLen), testKeyLen, testDepth),
	}
	for range 100 {
		n := len(trees)
		for range rand.IntN(20) {
			trees = append(trees, trees[rand.IntN(n)].Clone().(*fptree.FPTree))
		}
		for range rand.IntN(100) {
			trees[rand.IntN(len(trees))].RegisterKey(rangesync.RandomKeyBytes(testKeyLen))
		}

		trees = slices.DeleteFunc(trees, func(ft *fptree.FPTree) bool {
			if n == 1 {
				return false
			}
			n--
			if rand.IntN(3) == 0 {
				ft.Release()
				return true
			}
			return false
		})
		allTrees := slices.Clone(trees)
		for _, ft := range trees {
			allTrees = append(allTrees, ft.IDStore().(*fptree.FPTree))
		}
		fptree.AnalyzeTreeNodeRefs(t, allTrees...)
		for _, ft := range trees {
			fptree.CheckTree(t, ft)
			fptree.CheckTree(t, ft.IDStore().(*fptree.FPTree))
		}
		if t.Failed() {
			break
		}
	}
	for _, ft := range trees {
		ft.Release()
	}
	for _, ft := range trees {
		require.Zero(t, ft.PoolNodeCount())
	}
}

type hashList []rangesync.KeyBytes

func (l hashList) findGTE(h rangesync.KeyBytes) int {
	p, _ := slices.BinarySearchFunc(l, h, func(a, b rangesync.KeyBytes) int {
		return a.Compare(b)
	})
	return p
}

func (l hashList) keyAt(p int) rangesync.KeyBytes {
	if p == len(l) {
		p = 0
	}
	return rangesync.KeyBytes(l[p])
}

type fpResultWithBounds struct {
	fp rangesync.Fingerprint
	//nolint:unused
	count uint32
	itype int
	start rangesync.KeyBytes
	//nolint:unused
	next rangesync.KeyBytes
}

func toFPResultWithBounds(t *testing.T, fpr fptree.FPResult) fpResultWithBounds {
	return fpResultWithBounds{
		fp:    fpr.FP,
		count: fpr.Count,
		itype: fpr.IType,
		next:  fpr.Next,
		start: firstKey(t, fpr.Items),
	}
}

func dumbFP(hs hashList, x, y rangesync.KeyBytes, limit int) fpResultWithBounds {
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
		fpr.start = hs.keyAt(p)
		for {
			if p >= pY || limit == 0 {
				fpr.next = hs.keyAt(p)
				break
			}
			fpr.fp.Update(hs.keyAt(p))
			limit--
			fpr.count++
			p++
		}
	case 1:
		p := hs.findGTE(x)
		fpr.start = hs.keyAt(p)
		for {
			if p >= len(hs) || limit == 0 {
				fpr.next = hs.keyAt(p)
				break
			}
			fpr.fp.Update(hs.keyAt(p))
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
				fpr.next = hs.keyAt(p)
				break
			}
			fpr.fp.Update(hs.keyAt(p))
			limit--
			fpr.count++
			p++
		}
	default:
		pX := hs.findGTE(x)
		p := pX
		fpr.start = hs.keyAt(p)
		fpr.next = fpr.start
		for {
			if limit == 0 {
				fpr.next = hs.keyAt(p)
				break
			}
			fpr.fp.Update(hs.keyAt(p))
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

func verifyInterval(t *testing.T, hs hashList, ft *fptree.FPTree, x, y rangesync.KeyBytes, limit int) fptree.FPResult {
	expFPR := dumbFP(hs, x, y, limit)
	fpr, err := ft.FingerprintInterval(x, y, limit)
	require.NoError(t, err)
	require.Equal(t, expFPR, toFPResultWithBounds(t, fpr),
		"x=%s y=%s limit=%d", x.String(), y.String(), limit)

	require.Equal(t, expFPR, toFPResultWithBounds(t, fpr),
		"x=%s y=%s limit=%d", x.String(), y.String(), limit)

	return fpr
}

func verifySubIntervals(
	t *testing.T,
	hs hashList,
	ft *fptree.FPTree,
	x, y rangesync.KeyBytes,
	limit, d int,
) fptree.FPResult {
	fpr := verifyInterval(t, hs, ft, x, y, limit)
	if fpr.Count > 1 {
		c := int((fpr.Count + 1) / 2)
		if limit >= 0 {
			require.Less(t, c, limit)
		}
		part := verifyInterval(t, hs, ft, x, y, c)
		m := make(rangesync.KeyBytes, len(x))
		copy(m, part.Next)
		verifySubIntervals(t, hs, ft, x, m, -1, d+1)
		verifySubIntervals(t, hs, ft, m, y, -1, d+1)
	}
	return fpr
}

func testFPTreeManyItems(t *testing.T, trees []*fptree.FPTree, randomXY bool, numItems, maxDepth, repeat int) {
	ft := trees[0]
	hs := make(hashList, numItems)
	var fp rangesync.Fingerprint
	for i := range hs {
		h := rangesync.RandomKeyBytes(testKeyLen)
		hs[i] = h
		ft.RegisterKey(h)
		fp.Update(h)
	}
	fptree.AnalyzeTreeNodeRefs(t, trees...)
	slices.SortFunc(hs, func(a, b rangesync.KeyBytes) int {
		return a.Compare(b)
	})

	fptree.CheckTree(t, ft)
	for _, k := range hs {
		require.True(t, ft.CheckKey(k), "checkKey(%s)", k.ShortString())
	}

	fpr, err := ft.FingerprintInterval(hs[0], hs[0], -1)
	require.NoError(t, err)
	require.Equal(t, fp, fpr.FP, "fp")
	require.Equal(t, uint32(numItems), fpr.Count, "count")
	require.Equal(t, 0, fpr.IType, "itype")
	for i := 0; i < repeat; i++ {
		var x, y rangesync.KeyBytes
		if randomXY {
			x = rangesync.RandomKeyBytes(testKeyLen)
			y = rangesync.RandomKeyBytes(testKeyLen)
		} else {
			x = hs[rand.IntN(numItems)]
			y = hs[rand.IntN(numItems)]
		}
		verifySubIntervals(t, hs, ft, x, y, -1, 0)
	}
}

func repeatTestFPTreeManyItems(
	t *testing.T,
	makeFPTrees mkFPTreesFunc,
) {
	const (
		repeatOuter = 3
		repeatInner = 5
		numItems    = 1 << 10
		maxDepth    = 12
	)
	for _, tc := range []struct {
		name     string
		randomXY bool
	}{
		{
			name:     "bounds from the set",
			randomXY: false,
		},
		{
			name:     "random bounds",
			randomXY: true,
		},
	} {
		for i := 0; i < repeatOuter; i++ {
			testFPTreeManyItems(t, makeFPTrees(t), tc.randomXY, numItems, maxDepth, repeatInner)
		}
	}
}

func TestFPTreeManyItems(t *testing.T) {
	t.Run("values in fpTree", func(t *testing.T) {
		repeatTestFPTreeManyItems(t, makeFPTreeWithValues)
	})
	t.Run("in-memory fptree-based id store", func(t *testing.T) {
		repeatTestFPTreeManyItems(t, makeInMemoryFPTree)
	})
	t.Run("db-backed store", func(t *testing.T) {
		repeatTestFPTreeManyItems(t, makeDBBackedFPTree)
	})
}

func verifyEasySplit(
	t *testing.T,
	ft *fptree.FPTree,
	x, y rangesync.KeyBytes,
	depth,
	maxDepth int,
) (
	succeeded, failed int,
) {
	fpr, err := ft.FingerprintInterval(x, y, -1)
	require.NoError(t, err)
	if fpr.Count <= 1 {
		return 0, 0
	}
	a := firstKey(t, fpr.Items)
	require.NoError(t, err)
	b := fpr.Next
	require.NotNil(t, b)

	m := fpr.Count / 2
	sr, err := ft.EasySplit(x, y, int(m))
	if err != nil {
		require.ErrorIs(t, err, fptree.ErrEasySplitFailed)
		failed++
		sr, err = ft.Split(x, y, int(m))
		require.NoError(t, err)
	}
	require.NoError(t, err)
	require.NotZero(t, sr.Part0.Count)
	require.NotZero(t, sr.Part1.Count)
	require.Equal(t, fpr.Count, sr.Part0.Count+sr.Part1.Count)
	require.Equal(t, fpr.IType, sr.Part0.IType)
	require.Equal(t, fpr.IType, sr.Part1.IType)
	fp := sr.Part0.FP
	fp.Update(sr.Part1.FP[:])
	require.Equal(t, fpr.FP, fp)
	require.Equal(t, a, firstKey(t, sr.Part0.Items))
	precMiddle := firstKey(t, sr.Part1.Items)

	fpr11, err := ft.FingerprintInterval(x, precMiddle, -1)
	require.NoError(t, err)
	require.Equal(t, sr.Part0.Count, fpr11.Count)
	require.Equal(t, sr.Part0.FP, fpr11.FP)
	require.Equal(t, a, firstKey(t, fpr11.Items))

	fpr12, err := ft.FingerprintInterval(precMiddle, y, -1)
	require.NoError(t, err)
	require.Equal(t, sr.Part1.Count, fpr12.Count)
	require.Equal(t, sr.Part1.FP, fpr12.FP)
	require.Equal(t, precMiddle, firstKey(t, fpr12.Items))

	fpr11, err = ft.FingerprintInterval(x, sr.Middle, -1)
	require.NoError(t, err)
	require.Equal(t, sr.Part0.Count, fpr11.Count)
	require.Equal(t, sr.Part0.FP, fpr11.FP)
	require.Equal(t, a, firstKey(t, fpr11.Items))

	fpr12, err = ft.FingerprintInterval(sr.Middle, y, -1)
	require.NoError(t, err)
	require.Equal(t, sr.Part1.Count, fpr12.Count)
	require.Equal(t, sr.Part1.FP, fpr12.FP)
	require.Equal(t, precMiddle, firstKey(t, fpr12.Items))

	if maxDepth > 0 && depth >= maxDepth {
		return 1, 0
	}
	s1, f1 := verifyEasySplit(t, ft, x, sr.Middle, depth+1, maxDepth)
	s2, f2 := verifyEasySplit(t, ft, sr.Middle, y, depth+1, maxDepth)
	return succeeded + s1 + s2 + 1, failed + f1 + f2
}

func TestEasySplit(t *testing.T) {
	maxDepth := 17
	count := 10000
	for range 5 {
		store := fptree.NewFPTreeWithValues(10000, testKeyLen)
		ft := fptree.NewFPTree(10000, store, testKeyLen, maxDepth)
		for range count {
			h := rangesync.RandomKeyBytes(testKeyLen)
			ft.RegisterKey(h)
		}
		x := firstKey(t, ft.All()).Clone()
		x.Trim(maxDepth)

		succeeded, failed := verifyEasySplit(t, ft, x, x, 0, maxDepth-2)
		successRate := float64(succeeded) * 100 / float64(succeeded+failed)
		t.Logf("succeeded %d, failed %d, success rate %.2f%%",
			succeeded, failed, successRate)
		require.GreaterOrEqual(t, successRate, 95.0)
	}
}

func TestEasySplitFPTreeWithValues(t *testing.T) {
	count := 10000

	for range 5 {
		ft := fptree.NewFPTreeWithValues(10000, testKeyLen)
		for range count {
			h := rangesync.RandomKeyBytes(testKeyLen)
			ft.RegisterKey(h)
		}

		x := firstKey(t, ft.All()).Clone()
		_, failed := verifyEasySplit(t, ft, x, x, 0, -1)
		require.Zero(t, failed)
	}
}
