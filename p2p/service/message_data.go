package service

type Data interface {
	messageData()
	Bytes() []byte
}

type Data_Bytes struct {
	Payload []byte
}

type Data_MsgWrapper struct {
	Req     bool
	MsgType uint32
	ReqID   uint64
	Payload []byte
}

func (m Data_Bytes) messageData() {}

func (m Data_Bytes) Bytes() []byte {
	return m.Payload
}

func (m Data_MsgWrapper) messageData() {}

func (m Data_MsgWrapper) Bytes() []byte {
	return m.Payload
}
