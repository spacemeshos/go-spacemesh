package state

import "github.com/spacemeshos/go-spacemesh/common/types"

type Votes struct {
	Head, Tail *LayerVote
}

func (v *Votes) Append(lv *LayerVote) {
	if v.Head == nil && v.Tail == nil {
		v.Head = lv
		v.Tail = lv
	} else {
		v.Tail = v.Tail.Append(lv)
	}
}

func (v *Votes) CopyAfter(lid types.LayerID) *Votes {
	return v.CopyAt(lid.Add(1))
}

func (v *Votes) CopyAt(lid types.LayerID) *Votes {
	lv := v.Head.Find(lid)
	if lv == nil {
		return &Votes{}
	}
	tail := lv.Copy()
	tail.Prev = nil
	return &Votes{Tail: tail, Head: v.Head.Copy()}
}

type LayerVote struct {
	*LayerInfo
	Vote   Vote
	Blocks []BlockVote

	Prev, Next *LayerVote
}

func (l *LayerVote) Copy() *LayerVote {
	return &LayerVote{
		LayerInfo: l.LayerInfo,
		Vote:      l.Vote,
		Blocks:    l.Blocks,
		Prev:      l.Prev,
		Next:      l.Next,
	}
}

func (l *LayerVote) Find(lid types.LayerID) *LayerVote {
	for current := l; current != nil; current = current.Next {
		if l.ID == lid {
			return l
		}
	}
	return nil
}

func (l *LayerVote) Append(lv *LayerVote) *LayerVote {
	l.Next = lv.Next
	lv.Prev = l
	return lv
}
