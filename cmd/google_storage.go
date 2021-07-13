package cmd

import (
	"cloud.google.com/go/storage"
	"compress/gzip"
	"context"
	"encoding/json"
	"log"
	"os"
)


func download() {
	ctx := context.Background()

	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	// if load state from google storage
	if some_flag {
		// Sets the name for the new bucket.
		bucketName := os.Getenv("STATE_BUCKET")
		if bucketName == "" {

		}
	}
	// Creates a Bucket instance.
	bucket := client.Bucket(bucketName)

	obj := bucket.Object("bestbuy/products.json.gz").ReadCompressed(true) // see https://developer.bestbuy.com/apis
	rdr, err := obj.NewReader(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer rdr.Close()

	gzr, err := gzip.NewReader(rdr)
	if err != nil {
		log.Fatal(err)
	}
	defer gzr.Close()

	decoder := json.NewDecoder(gzr)
	decoder.UseNumber()

}