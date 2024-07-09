package main

import (
	"context"
	"errors"
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
		fmt.Println(`Usage:
	> activeset <publish epoch> <db path>
Example:
	query atxs that are published in epoch 3 and stored in state.sql file.
	> activeset 3 state.sql`)
		flag.PrintDefaults()
	}
	flag.Parse()

	publish, err := strconv.Atoi(flag.Arg(0))
	must(err, "publish epoch %v is not a valid integer: %s", flag.Arg(0), err)
	dbpath := flag.Arg(1)
	if len(dbpath) == 0 {
		must(errors.New("dbpath is empty"), "dbpath is empty\n")
	}
	db, err := sql.Open("file:" + dbpath)
	must(err, "can't open db at dbpath=%v. err=%s\n", dbpath, err)

	ids, err := atxs.GetIDsByEpoch(context.Background(), db, types.EpochID(publish))
	must(err, "get ids by epoch %d. dbpath=%v. err=%s\n", publish, dbpath, err)
	var weight uint64
	for _, id := range ids {
		atx, err := atxs.Get(db, id)
		must(err, "get id %v: %s\n", id, err)
		weight += atx.Weight
	}
	fmt.Printf("count = %d\nweight = %d\n", len(ids), weight)
}

func must(err error, msg string, vars ...any) {
	if err != nil {
		fmt.Printf(msg, vars...)
		fmt.Println("")
		flag.Usage()
		os.Exit(1)
	}
}
