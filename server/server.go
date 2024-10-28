package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"path"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"gocloud.dev/blob"
)

const (
	RFC2822 string = "Mon, 02 Jan 2006 15:04:05 MST"
)

var (
	audioRegexp       = regexp.MustCompile(`"(https?://[^ ]+\.mp3)"`)
	imageRegexp       = regexp.MustCompile(`<img src="(https?:\/\/[^ ]*)"`)
	descriptionRegexp = regexp.MustCompile(`Hi[ ]+Connor, ([^<]*)<\/p>`)
)

type Server struct {
	template *template.Template
	bucket   *blob.Bucket
}

func NewServer(templatePath string, bucket *blob.Bucket) (*Server, error) {
	xt := template.New("text").Funcs(template.FuncMap{
		"rfc2822": func(t time.Time) string {
			return t.Format(RFC2822)
		},
		"rfc3339": func(t time.Time) string {
			return t.Format(time.RFC3339)
		},
		"timestamp": func(t time.Time) string {
			return t.Format(time.RFC3339)
		},
	})
	_, err := xt.ParseGlob(path.Join(templatePath, "*.xml.tmpl"))
	if err != nil {
		return nil, fmt.Errorf("parse template at `%s`: %w", templatePath, err)
	}
	return &Server{template: xt, bucket: bucket}, nil
}

func (s *Server) GetFeed(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	blobReader, err := s.bucket.NewReader(ctx, fmt.Sprintf("%s/feed.xml", req.PathValue("feed")), nil)
	if err != nil {
		if err != nil {
			http.Error(w, "Could not access feed", http.StatusInternalServerError)
			log.Printf("construct object reader: %v", err)
			return
		}
	}
	defer blobReader.Close()

	w.Header().Add("Content-Type", "application/xml+rss;charset=UTF-8")
	w.Header().Add("Cache-Control", "no-cache")
	http.ServeContent(w, req, "", blobReader.ModTime(), blobReader)
}

type email struct {
	UUID        string    `json:"uuid"`
	Subject     string    `json:"subject"`
	Description string    `json:"description"`
	Date        time.Time `json:"date"`
	ImageURL    string    `json:"imageURL"`
	AudioURL    string    `json:"audioURL"`
	AudioSize   int       `json:"audioSize"`
}

func NewEmail(msg *mail.Message) (*email, error) {
	date, err := msg.Header.Date()
	if err != nil {
		return nil, fmt.Errorf("retrieve date header: %w", err)
	}

	dec := new(mime.WordDecoder)
	subject, err := dec.DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		return nil, fmt.Errorf("decode Subject of message using RFC 2047: %w", err)
	}

	html, err := messageMIME(msg, "text/html")
	if err != nil {
		return nil, fmt.Errorf("find HTML MIME portion of message body: %w", err)
	}
	sb := new(strings.Builder)
	_, err = io.Copy(sb, html)
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

	audioResponse, err := http.Head(audioURL)
	if err != nil {
		return nil, fmt.Errorf("HEAD audio url: %w", err)
	}
	defer audioResponse.Body.Close()
	audioSize, err := strconv.Atoi(audioResponse.Header.Get("Content-Length"))
	if err != nil {
		return nil, fmt.Errorf("fetch size of audio: %w", err)
	}

	return &email{
		UUID:        msg.Header.Get("X-Apple-UUID"),
		Subject:     subject,
		Description: description,
		Date:        date,
		ImageURL:    imageURL,
		AudioURL:    audioURL,
		AudioSize:   audioSize,
	}, nil
}

func (s *Server) AddEmail(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	msg, err := mail.ReadMessage(req.Body)
	if err != nil {
		http.Error(w, "Could not parse message", http.StatusBadRequest)
		log.Printf("parse message: %v", err)
		return
	}
	date, err := msg.Header.Date()
	if err != nil {
		http.Error(w, "Could not parse Date header", http.StatusBadRequest)
		log.Printf("parse date of message: %v", err)
		return
	}
	key := fmt.Sprintf("%s/items/%s.json", req.PathValue("feed"), date.Format(time.RFC3339))

	exists, err := s.bucket.Exists(ctx, key)
	if err != nil {
		http.Error(w, "Check if item exists", http.StatusInternalServerError)
		log.Printf("check if item exists: %v", err)
		return
	}
	if exists {
		http.Error(w, "An item already exists for this feed and timestamp", http.StatusConflict)
		return
	}

	item, err := NewEmail(msg)
	if err != nil {
		http.Error(w, "Could not parse email", http.StatusBadRequest)
		log.Printf("parse email: %v", err)
		return
	}

	// Write an item file
	itemWriter, err := s.bucket.NewWriter(ctx, key, nil)
	if err != nil {
		http.Error(w, "Could not construct object writer", http.StatusBadRequest)
		log.Printf("new object writer: %v", err)
		return
	}
	defer itemWriter.Close()
	err = json.NewEncoder(itemWriter).Encode(item)
	if err != nil {
		http.Error(w, "Could not write to item file", http.StatusBadRequest)
		log.Printf("write email to items file: %v", err)
		return
	}
	err = itemWriter.Close()
	if err != nil {
		http.Error(w, "Could not close item file", http.StatusBadRequest)
		log.Printf("close items file: %v", err)
		return
	}

	// Read all items file
	var items []email
	iter := s.bucket.List(&blob.ListOptions{Prefix: fmt.Sprintf("%s/items/", req.PathValue("feed"))})
	for {
		f, err := iter.Next(ctx)
		if err != nil {
			if err == io.EOF {
				break
			}
			http.Error(w, "List items files", http.StatusBadRequest)
			log.Printf("list items file: %v", err)
			return
		}

		itemReader, err := s.bucket.NewReader(ctx, f.Key, nil)
		if err != nil {
			http.Error(w, "Could not read item file", http.StatusInternalServerError)
			log.Printf("construct item object reader: %v", err)
			return
		}
		defer itemReader.Close()

		var item email
		err = json.NewDecoder(itemReader).Decode(&item)
		if err != nil {
			http.Error(w, "Could not parse item file", http.StatusInternalServerError)
			log.Printf("parse email from item file: %v", err)
			return
		}
		// Prepend item, so that the most recent is first
		items = append(items, item)
		copy(items[1:], items)
		items[0] = item

		err = itemReader.Close()
		if err != nil {
			http.Error(w, "Could not close item file", http.StatusInternalServerError)
			log.Printf("close item file: %v", err)
			return
		}
	}

	// Write a new feed file
	feedWriter, err := s.bucket.NewWriter(ctx, fmt.Sprintf("%s/feed.xml", req.PathValue("feed")), nil)
	if err != nil {
		http.Error(w, "Could not construct object writer", http.StatusBadRequest)
		log.Printf("new object writer: %v", err)
		return
	}
	defer feedWriter.Close()
	err = s.template.ExecuteTemplate(feedWriter, "feed.xml.tmpl", items)
	if err != nil {
		http.Error(w, "Could not generate new feed file", http.StatusBadRequest)
		log.Printf("execute feed template: %v", err)
		return
	}
	err = feedWriter.Close()
	if err != nil {
		http.Error(w, "Could not close feed file", http.StatusBadRequest)
		log.Printf("close feed file: %v", err)
		return
	}

	// Return parsed email representation
	w.WriteHeader(http.StatusCreated)
	err = json.NewEncoder(w).Encode(item)
	if err != nil {
		http.Error(w, "Could not serialize email as JSON", http.StatusBadRequest)
		log.Printf("encode email as json: %v", err)
		return
	}
}

// Find and parse part of message
func messageMIME(message *mail.Message, contentType string) (io.Reader, error) {
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("parse message content type: %w", err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, fmt.Errorf("expected multipart message but found %s", mediaType)
	}
	reader := multipart.NewReader(message.Body, params["boundary"])
	if reader == nil {
		return nil, fmt.Errorf("could not construct multipart reader for message")
	}
	for {
		part, err := reader.NextPart()
		if err != nil {
			return nil, fmt.Errorf("could not find %s part of message: %w", contentType, err)
		}
		mediaType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			return nil, fmt.Errorf("parse multipart message part content type: %w", err)
		}
		if mediaType == contentType {
			enc := strings.ToLower(part.Header.Get("Content-Transfer-Encoding"))
			switch enc {
			case "base64":
				return base64.NewDecoder(base64.StdEncoding, part), nil
			case "quoted-printable":
				return quotedprintable.NewReader(part), nil
			default:
				return part, nil
			}
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{feed}/feed.xml", s.GetFeed)
	mux.HandleFunc("POST /{feed}/email", s.AddEmail)
	mux.ServeHTTP(w, r)
}
