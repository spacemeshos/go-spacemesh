package net

// HandshakeData is the handshake message struct
type HandshakeData struct {
	ClientVersion string
	NetworkID     int32
	Port          uint16
}
