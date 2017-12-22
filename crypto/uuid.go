package crypto

import "github.com/google/uuid"

func UUIDString() string {
	return uuid.New().String()
}

func UUID() []byte {
	return []byte(uuid.New().String())
}
