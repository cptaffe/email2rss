package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/cptaffe/email2rss/internal/server"
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/gcsblob"
)

var (
	templatePath = flag.String("templates", "", "Path to the templates folder")
)

func main() {
	flag.Parse()
	ctx := context.Background()
	signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)

	bucket, err := blob.OpenBucket(ctx, "gs://connor.zip")
	if err != nil {
		log.Fatalf("open bucket: %v", err)
		return
	}
	defer bucket.Close()

	s, err := server.NewServer(ctx, *templatePath, bucket)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}
	http.ListenAndServe("0.0.0.0:8080", s)
}
