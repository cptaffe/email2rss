package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/mail"
	"os"

	"github.com/cptaffe/email2rss/internal/journalclub"
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

	jc, err := journalclub.FromMessage(msg)
	if err != nil {
		log.Fatalf("construct journalclub message: %v", err)
	}
	err = json.NewEncoder(os.Stdout).Encode(&jc)
	if err != nil {
		log.Fatalf("serialize journalclub message as JSON: %v", err)
	}
}
