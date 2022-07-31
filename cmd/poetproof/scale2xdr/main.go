package main

import (
	"fmt"
	"log"
	"os"

	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) < 2 {
		fmt.Printf("Please provide file to reencode")
		os.Exit(0)
	}
	filePath := os.Args[1]

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed opening file: %w", err)
	}

	var poetProof types.PoetProof
	var decodable scale.Decodable = &poetProof
	_, err = decodable.DecodeScale(scale.NewDecoder(file))
	if err != nil {
		return fmt.Errorf("decodable.DecodeScale failed: %w", err)
	}

	filePathXdr := filePath + ".xdr"
	fmt.Printf("Converting to %s\n", filePathXdr)

	fileTo, err := os.Create(filePathXdr)
	if err != nil {
		return fmt.Errorf("failed creating scale file to write reencode result to: %w", err)
	}

	_, err = xdr.Marshal(fileTo, &poetProof)
	if err != nil {
		return fmt.Errorf("xdr.Marshal failed: %w", err)
	}

	return nil
}
