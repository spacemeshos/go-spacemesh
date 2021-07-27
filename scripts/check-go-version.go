package main

import (
	"flag"
	"os"
	"runtime"
	"strconv"
	"strings"
)

var minMajor int
var minMinor int

func init() {
	flag.IntVar(&minMajor, "major", 1, "required major version")
	flag.IntVar(&minMinor, "minor", 14, "required minor version")
}

func main() {
	flag.Parse()
	ver := strings.Split(runtime.Version()[2:], ".")
	major, _ := strconv.Atoi(ver[0])
	minor, _ := strconv.Atoi(ver[1])
	if major < minMajor || (major == minMajor && minor < minMinor) {
		os.Exit(1)
	}
	os.Exit(0)
}
