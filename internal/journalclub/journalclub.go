package journalclub

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cptaffe/email2rss/internal/backend"
	"github.com/cptaffe/email2rss/internal/email"
)

var (
	audioRegexp                       = regexp.MustCompile(`"(https?://[^ ]+\.mp3)"`)
	imageRegexp                       = regexp.MustCompile(`<img src="(https?://[^ ]*)"`)
	descriptionRegexp                 = regexp.MustCompile(`Hi[ ]+Connor, (.*)</p>`)
	paperRegexp                       = regexp.MustCompile(`<a [^>]*href="(https?://(\w+\.)?doi.org[^"]*)"[^>]*>`)
	_                 backend.Item    = &Message{}
	_                 backend.Backend = &Backend{}
)

type Message struct {
	UUID        string    `json:"uuid"`
	Subject     string    `json:"subject"`
	Description string    `json:"description"`
	Date        time.Time `json:"date"`
	ImageURL    string    `json:"imageURL"`
	AudioURL    string    `json:"audioURL"`
	AudioSize   int       `json:"audioSize"`
	PaperURL    string    `json:"paperURL"`
}

func (msg *Message) Key() string {
	return msg.Date.Format(time.RFC3339)
}

func (msg *Message) Encode(w io.Writer) error {
	return json.NewEncoder(w).Encode(msg)
}

type Backend struct{}

func (b *Backend) Name() string {
	return "journalclub"
}

func (b *Backend) TemplatePath() string {
	return "journalclub.xml.tmpl"
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

	var audioURL string
	matches := audioRegexp.FindStringSubmatch(body)
	if matches != nil {
		audioURL = matches[1]
	}
	var imageURL string
	matches = imageRegexp.FindStringSubmatch(body)
	if matches != nil {
		imageURL = matches[1]
	}
	var description string
	matches = descriptionRegexp.FindStringSubmatch(body)
	if matches != nil {
		description = strings.TrimSpace(matches[1])
		// Capitalize
		description = strings.ToUpper(description[0:1]) + description[1:]
	}
	var paperURL string
	matches = paperRegexp.FindStringSubmatch(body)
	if matches != nil {
		paperURL = matches[1]
	}

	audioResponse, err := http.Head(audioURL)
	if err != nil {
		return nil, fmt.Errorf("HEAD audio url: %w", err)
	}
	defer audioResponse.Body.Close()
	audioSize, err := strconv.Atoi(audioResponse.Header.Get("Content-Length"))
	if err != nil {
		return nil, fmt.Errorf("fetch size of audio: %w", err)
	}

	return &Message{
		UUID:        msg.Header.Get("X-Apple-UUID"),
		Subject:     subject,
		Description: description,
		Date:        date,
		ImageURL:    imageURL,
		AudioURL:    audioURL,
		AudioSize:   audioSize,
		PaperURL:    paperURL,
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
