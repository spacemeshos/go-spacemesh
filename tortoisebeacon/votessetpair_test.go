package tortoisebeacon

//func Test_votesSetPair_Diff(t *testing.T) {
//	t.Parallel()
//
//	r := require.New(t)
//
//	tt := []struct {
//		name         string
//		firstRound   votesSetPair
//		currentRound votesSetPair
//		votesFor     proposalList
//		votesAgainst proposalList
//	}{
//		{
//			name: "Case 1",
//			firstRound: votesSetPair{
//				ValidVotes: map[types.Hash32]struct{}{
//					types.HexToHash32("0xA"): {},
//					types.HexToHash32("0xB"): {},
//				},
//				InvalidVotes: map[types.Hash32]struct{}{
//					types.HexToHash32("0xC"): {},
//				},
//			},
//			currentRound: votesSetPair{
//				ValidVotes: map[types.Hash32]struct{}{
//					types.HexToHash32("0xA"): {},
//				},
//				InvalidVotes: map[types.Hash32]struct{}{
//					types.HexToHash32("0xB"): {},
//					types.HexToHash32("0xC"): {},
//				},
//			},
//			votesFor:     proposalList{},
//			votesAgainst: proposalList{types.HexToHash32("0xB")},
//		},
//		{
//			name: "Case 2",
//			firstRound: votesSetPair{
//				ValidVotes: map[types.Hash32]struct{}{
//					types.HexToHash32("0x0"): {},
//					types.HexToHash32("0x1"): {},
//					types.HexToHash32("0x2"): {},
//					types.HexToHash32("0x3"): {},
//					types.HexToHash32("0x4"): {},
//				},
//				InvalidVotes: map[types.Hash32]struct{}{
//					types.HexToHash32("0x5"): {},
//					types.HexToHash32("0x6"): {},
//					types.HexToHash32("0x7"): {},
//					types.HexToHash32("0x8"): {},
//					types.HexToHash32("0x9"): {},
//				},
//			},
//			currentRound: votesSetPair{
//				ValidVotes: map[types.Hash32]struct{}{
//					types.HexToHash32("0x0"): {},
//					types.HexToHash32("0x1"): {},
//					types.HexToHash32("0x8"): {},
//					types.HexToHash32("0x9"): {},
//				},
//				InvalidVotes: map[types.Hash32]struct{}{
//					types.HexToHash32("0x2"): {},
//					types.HexToHash32("0x3"): {},
//					types.HexToHash32("0x4"): {},
//					types.HexToHash32("0x5"): {},
//					types.HexToHash32("0x6"): {},
//					types.HexToHash32("0x7"): {},
//				},
//			},
//			votesFor:     proposalList{types.HexToHash32("0x8"), types.HexToHash32("0x9")},
//			votesAgainst: proposalList{types.HexToHash32("0x2"), types.HexToHash32("0x3"), types.HexToHash32("0x4")},
//		},
//	}
//
//	for _, tc := range tt {
//		tc := tc
//		t.Run(tc.name, func(t *testing.T) {
//			t.Parallel()
//
//			votesFor, votesAgainst := tc.currentRound.Diff(tc.firstRound)
//			r.EqualValues(tc.votesFor.Sort(), votesFor.Sort())
//			r.EqualValues(tc.votesAgainst.Sort(), votesAgainst.Sort())
//		})
//	}
//}
