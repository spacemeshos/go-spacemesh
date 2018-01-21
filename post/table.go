package post

const (
	s = 65536 // 137438953472 // number of entries (factor 1) - must be 2^H for some H
	e = 8     // bits - entry size
)

// A simple binary data table backed by a data file
type Table interface {
	read(off int64, out []byte) error // read len(out) bytes at offset off from the table
	seek(off int64) error
	write(data []byte) error // write data at current offset and increases offset
	sync() error             // syncs all writes to disk
	deleteAllData() error    // delete all existing data from the table
}

type tableImpl struct {
	id       string
	size     int64
	entries  int64
	dir      string
	dataFile dataFile
}

// Creates a new post data table
// id: node id - base58 encoded key
// dir: node data dir directory
// mul: table size factor, e.g. 2 for x2 of min table size of S*E
func NewTable(mul int, id string, dir string) (Table, error) {
	size := s * int64(mul)
	entries := size / e
	dataFile := newDataFile(dir)

	// for now always delete the file if it exists and create a new one
	if dataFile.exists() {
		err := dataFile.delete()
		if err != nil {
			return nil, err
		}
	}

	if !dataFile.exists() {
		err := dataFile.create()
		if err != nil {
			return nil, err
		}
	}

	t := &tableImpl{id: id, size: size, entries: entries, dir: dir, dataFile: dataFile}

	return t, nil
}

// Deletes all data
func (d *tableImpl) deleteAllData() error {
	err := d.dataFile.delete()
	if err != nil {
		return err
	}
	return d.dataFile.create()
}

// Writes data at current table offset
func (d *tableImpl) write(data []byte) error {
	return d.dataFile.write(data)
}

// Seeks to a new offset relative to 0
func (d *tableImpl) seek(off int64) error {
	return d.dataFile.seek(off)
}

// Saves all writes to data file on disk
func (d *tableImpl) sync() error {
	return d.dataFile.sync()
}

// Reads len(out) bytes at offset
func (d *tableImpl) read(off int64, out []byte) error {
	return d.dataFile.read(off, out)
}
