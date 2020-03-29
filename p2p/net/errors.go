package net

// Temporary checks whether the given error should be considered temporary.
func Temporary(err error) bool {
	tErr, ok := err.(interface {
		Temporary() bool
	})
	return ok && tErr.Temporary()
}

// ConnectionWithErr is a pair of Connection and an error occurred within the connection
type ConnectionWithErr struct {
	Conn Connection
	Err  error
}
