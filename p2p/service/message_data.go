package service

type MessageData interface {
	messageData()
	Bytes() []byte
}

type MessageData_Bytes struct {
	Payload []byte
}

type MessageData_MsgWrapper struct {
	Req     bool
	MsgType uint32
	ReqID   uint64
	Payload []byte
}

func (m MessageData_Bytes) messageData() {}

func (m MessageData_Bytes) Bytes() []byte {
	return m.Payload
}

func (m MessageData_MsgWrapper) messageData() {}

func (m MessageData_MsgWrapper) Bytes() []byte {
	return m.Payload
}
