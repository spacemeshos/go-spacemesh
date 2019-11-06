package net

type HandshakeData struct {
	ClientVersion string
	NetworkID     int32
	Port          uint16
}
