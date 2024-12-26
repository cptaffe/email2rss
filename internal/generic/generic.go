package generic

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/cptaffe/email2rss/internal/backend"
	"github.com/cptaffe/email2rss/internal/email"
)

var (
	_ backend.Item    = &Message{}
	_ backend.Backend = &Backend{}
)

type Message struct {
	UUID    string    `json:"uuid"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
	Body    string    `json:"body"`
}

func (msg *Message) Key() string {
	return msg.Date.Format(time.RFC3339)
}

func (msg *Message) Encode(w io.Writer) error {
	return json.NewEncoder(w).Encode(msg)
}

type Backend struct {
	name string
}

func NewBackend(feed string) *Backend {
	return &Backend{name: feed}
}

func (b *Backend) Name() string {
	return b.name
}

func (b *Backend) TemplatePath() string {
	return "generic.xml.tmpl"
}

func (b *Backend) FromMessage(msg *mail.Message) (backend.Item, error) {
	date, err := msg.Header.Date()
	if err != nil {
		return nil, fmt.Errorf("retrieve date header: %w", err)
	}

	dec := new(mime.WordDecoder)
	subject, err := dec.DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		return nil, fmt.Errorf("decode Subject of message using RFC 2047: %w", err)
	}

	html, err := email.MessageMIME(msg, "text/html")
	if err != nil {
		return nil, fmt.Errorf("find HTML MIME portion of message body: %w", err)
	}
	var sb strings.Builder
	_, err = io.Copy(&sb, html)
	if err != nil {
		return nil, fmt.Errorf("read HTML as string: %w", err)
	}
	body := sb.String()

	return &Message{
		UUID:    msg.Header.Get("X-Apple-UUID"),
		Subject: subject,
		Date:    date,
		Body:    body,
	}, nil
}

func (b *Backend) Decode(r io.Reader) (backend.Item, error) {
	var item Message
	err := json.NewDecoder(r).Decode(&item)
	if err != nil {
		return nil, fmt.Errorf("parse item from JSON file: %w", err)
	}
	return &item, nil
}
