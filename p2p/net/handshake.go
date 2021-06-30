package net

// HandshakeData is the handshake message struct
type HandshakeData struct {
	ClientVersion string
	NetworkID     uint32
	Port          uint16
}
