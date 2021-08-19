package peersync

import (
	"sort"
	"time"
)

type timedResponse struct {
	Response
	receiveTimestamp int64
}

type round struct {
	ID                uint64
	Timestamp         int64
	RequiredResponses int
	responses         []timedResponse
}

func (r *round) AddResponse(resp Response, timestamp int64) {
	if resp.ID != r.ID {
		return
	}
	r.responses = append(r.responses,
		timedResponse{Response: resp, receiveTimestamp: timestamp})
}

func (r *round) Ready() bool {
	return len(r.responses) >= r.RequiredResponses
}

func (r *round) Offset() time.Duration {
	if len(r.responses) == 0 {
		return 0
	}
	offsets := make([]int64, len(r.responses))
	for i := range r.responses {
		rtt := r.responses[i].receiveTimestamp - r.Timestamp
		offsets[i] = int64(r.responses[i].Timestamp) - r.Timestamp - rtt/2
	}
	sort.Slice(offsets, func(i, j int) bool {
		return offsets[i] < offsets[j]
	})
	if len(offsets)%2 == 0 {
		mid := len(offsets) / 2
		return time.Duration(offsets[mid-1]+offsets[mid]) / 2
	}
	return time.Duration(offsets[len(offsets)/2])
}
