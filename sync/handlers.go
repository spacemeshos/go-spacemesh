package sync

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/sync/pb"
)

func HandleBlockResponse(msg []byte) interface{} {
	data := &pb.FetchBlockResp{}
	err := proto.Unmarshal(msg, data)
	if err != nil {
		return nil
	}
	return &BlockMock{data.BlockId, int(data.Layer)} //todo add data to block
}

// Handle an incoming pong message from a remote node
func HandleLayerHashResponse(msg []byte) interface{} {
	data := &pb.LayerHashResp{}
	err := proto.Unmarshal(msg, data)
	if err != nil {
		return nil
	}
	return data.Hash
}
