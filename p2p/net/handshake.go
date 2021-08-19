package net

// HandshakeData is the handshake message struct
type HandshakeData struct {
	ClientVersion string `ssz-max:"1024"`
	NetworkID     uint32
	Port          uint16
}
