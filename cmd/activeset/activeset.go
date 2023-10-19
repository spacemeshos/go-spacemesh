package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func main() {
	flag.Usage = func() {
		fmt.Println("activeset <publish epoch> <db path>")
		flag.PrintDefaults()
	}
	flag.Parse()

	publish, err := strconv.Atoi(flag.Arg(0))
	must(err, "publish epoch %v is not a valid integer: %s", flag.Arg(0), err)
	dbpath := flag.Arg(1)
	db, err := sql.Open("file:" + dbpath)
	must(err, "can't open db at dbpath=%v. err=%s\n", dbpath, err)

	ids, err := atxs.GetIDsByEpoch(db, types.EpochID(publish))
	must(err, "get ids by epoch %d. dbpath=%v. err=%s\n", publish, dbpath, err)
	var weight uint64
	for _, id := range ids {
		atx, err := atxs.Get(db, id)
		must(err, "get id %v: %s\n", id, err)
		weight += atx.GetWeight()
	}
	fmt.Printf("count = %d\nweight = %d\n", len(ids), weight)
}

func must(err error, msg string, vars ...any) {
	if err != nil {
		fmt.Printf(msg, vars...)
		os.Exit(1)
	}
}
