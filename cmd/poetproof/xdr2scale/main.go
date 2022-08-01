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
	_, err = xdr.Unmarshal(file, &poetProof)
	if err != nil {
		return fmt.Errorf("xdr.Unmarshal failed: %w", err)
	}

	filePathScale := filePath + ".scale"
	fmt.Printf("Converting to %s\n", filePathScale)

	fileTo, err := os.Create(filePathScale)
	if err != nil {
		return fmt.Errorf("failed creating scale file to write reencode result to: %w", err)
	}

	var encodable scale.Encodable = &poetProof

	_, err = encodable.EncodeScale(scale.NewEncoder(fileTo))
	if err != nil {
		return fmt.Errorf("encodable.EncodeScale failed: %w", err)
	}

	return nil
}
