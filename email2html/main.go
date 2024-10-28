package main

import (
	"flag"
	"io"
	"log"
	"net/mail"
	"os"

	"github.com/cptaffe/email2rss/internal/email"
)

var (
	contentType = flag.String("mime", "text/html", "MIME type to extract from an email input")
)

func main() {
	flag.Parse()
	msg, err := mail.ReadMessage(os.Stdin)
	if err != nil {
		log.Fatalf("parse message: %v", err)
	}

	r, err := email.MessageMIME(msg, *contentType)
	if err != nil {
		log.Fatalf("find MIME portion of message: %v", err)
	}
	io.Copy(os.Stdout, r)
}
