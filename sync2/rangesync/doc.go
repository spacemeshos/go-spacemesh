// Package rangesync implements pairwise set reconciliation protocol.
//
// The protocol is based on this paper:
// Range-Based Set Reconciliation by Aljoscha Meyer
// https://arxiv.org/pdf/2212.13567.pdf
//
// The protocol has an advantage of a possibility to build a reusable synchronization
// helper structures (ordered sets) and also a possibility to use the same structure to
// reconcile subranges of the ordered sets efficiently against multiple peers.
// The disadvantage is that the algorithm is not very efficient for large differences
// (like tens of thousands of elements) when the elements have no apparent ordering, which
// is the case with hash-based IDs like ATX ids. In order to mitigate this problem, the
// protocol supports exchanging recently-received items between peers before beginning
// the actual reconciliation.
// When the difference is large, the algorithm can automatically degrade to dumb "send me
// the whole set" mode. The resulting increased load on the network and the peer computers
// is mitigated by splitting the element range between multiple peers.
//
// The core concepts are the following:
//  1. Ordered sets. The sets to be reconciled are ordered. The elements in the set are
//     also called "keys" b/c they represent the IDs of the actual objects being
//     synchronized, such as ATXs.
//  2. Ranges. [x, y) range denotes a range of items in the set, where x is inclusive and y
//     is exclusive. The ranges may wrap around. This means:
//     - x == y: the whole set
//     - x < y: a normal range starting with x and ending below y
//     - x > y: a wrapped around range, that is from x (inclusive) to the end of the set and
//     from the beginning of the set to y, non-inclusive.
//  3. Fingerprint. Each range has a fingerprint, which is all the IDs (keys) in the range
//     XORed together. The fingerprint is used to quickly check if the range is in sync
//     with the peer.
//
// Each OrderedSet is supposed to be able to provide the number of items and the
// fingerprint of items in a range relatively cheaply.
// Additionally, the OrderedSet has Recent method that retrieves recently-based keys
// (elements) based on the provided timestamp. This reconciliation helper mechanism is
// optional, so Recent may just return an empty sequence.
//
// Below is a log of a sample interaction between two peers A and B.
// Interactions:
//
// A: empty set; B: empty set
// A -> B:
//
//	EmptySet
//	EndRound
//
// B -> A:
//
//	Done
//
// A: empty set; B: non-empty set
// A -> B:
//
//	EmptySet
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//
// A -> B:
//
//	Done
//
// A: small set (< maxSendRange); B: non-empty set
// A -> B:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, y)
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//
// A -> B:
//
//	Done
//
// A: large set; B: non-empty set; maxDiff < 0
// A -> B:
//
//	Fingerprint [x, y)
//	EndRound
//
// B -> A:
//
//	Fingerprint [x, m)
//	Fingerprint [m, y)
//	EndRound
//
// A -> B:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, m)
//	EndRound
//
// A -> B:
//
//	Done
//
// A: large set; B: non-empty set; maxDiff >= 0; differenceMetric <= maxDiff
// NOTE: Sample includes fingerprint
// A -> B:
//
//	Sample [x, y)
//	EndRound
//
// B -> A:
//
//	Fingerprint [x, m)
//	Fingerprint [m, y)
//	EndRound
//
// A -> B:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, m)
//	EndRound
//
// A -> B:
//
//	Done
//
// A: large set; B: non-empty set; maxDiff >= 0; differenceMetric > maxDiff
// A -> B:
//
//	Sample [x, y)
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, y)
//	EndRound
//
// A -> B:
//
//	Done
//
// A: large set; B: non-empty set; sync priming; maxDiff >= 0; differenceMetric <= maxDiff (after priming)
// A -> B:
//
//	ItemBatch
//	ItemBatch
//	...
//	Recent
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//	Sample [x, y)
//	EndRound
//
// A -> B:
//
//	Fingerprint [x, m)
//	Fingerprint [m, y)
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, m)
//	EndRound
//
// A -> B:
//
//	Done
//
// A: large set; B: non-empty set; sync priming; maxDiff < 0
// A -> B:
//
//	ItemBatch
//	ItemBatch
//	...
//	Recent
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//	Fingerprint [x, y)
//	EndRound
//
// A -> B:
//
//	Fingerprint [x, m)
//	Fingerprint [m, y)
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, m)
//	EndRound
//
// A -> B:
//
//	Done
package rangesync
