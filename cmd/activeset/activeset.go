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
	dbpath := flag.String("db", "", "path to sqlite where atxs are stored")
	flag.Usage = func() {
		fmt.Println("activeset <publish epoch>")
		flag.PrintDefaults()
	}
	flag.Parse()

	publish, err := strconv.Atoi(flag.Arg(0))
	must(fmt.Sprintf("publish epoch %v is not a valid integer: %s", flag.Arg(0), err), err)
	db, err := sql.Open("file:" + *dbpath)
	must(fmt.Sprintf("can't open db at dbpath=%v. err=%s\n", dbpath, err), err)

	ids, err := atxs.GetIDsByEpoch(db, types.EpochID(publish))
	must(fmt.Sprintf("get ids by epoch %d. dbpath=%v. err=%s\n", publish, *dbpath, err), err)
	var weight uint64
	for _, id := range ids {
		atx, err := atxs.Get(db, id)
		must(fmt.Sprintf("get id %v: %s\n", id, err), err)
		weight += atx.GetWeight()
	}
	fmt.Printf("count = %d\nweight = %d\n", len(ids), weight)
}

func must(msg string, err error) {
	if err != nil {
		fmt.Println(msg)
		os.Exit(1)
	}
}
