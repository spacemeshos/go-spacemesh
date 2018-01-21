package post

const (
	s = 65536 // 137438953472 // number of entries (factor 1) - must be 2^n for some n
	e = 8     // bits - entry size
)

type Table interface {
	read(off int64, out []byte) error // read len(out) bytes at offset off from the table
	seek(off int64) error
	write(data []byte) error // write data at current offset and increases offset
	sync() error
	deleteAllData() error // delete all existing data from the table
}

type tableImpl struct {
	id       string
	size     int64
	entries  int64
	dir      string
	dataFile dataFile
}

// Creates a new post table
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
func (d *tableImpl) deleteAllData() error {
	err := d.dataFile.delete()
	if err != nil {
		return err
	}
	return d.dataFile.create()
}

func (d *tableImpl) write(data []byte) error {
	return d.dataFile.write(data)
}

func (d *tableImpl) seek(off int64) error {
	return d.dataFile.seek(off)
}

func (d *tableImpl) sync() error {
	return d.dataFile.sync()
}

func (d *tableImpl) read(off int64, out []byte) error {
	return d.dataFile.read(off, out)
}
