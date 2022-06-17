package datastore

// Getter wraps the database read operation.
type Getter interface {
	Get(Hint, []byte) ([]byte, error)
}
