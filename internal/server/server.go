package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"path"
	"text/template"
	"time"

	"github.com/cptaffe/email2rss/internal/journalclub"
	"gocloud.dev/blob"
)

const (
	RFC2822 string = "Mon, 02 Jan 2006 15:04:05 MST"
)

type Server struct {
	template *template.Template
	bucket   *blob.Bucket
}

// TODO: Abstract the implementation of email -> item state and item states -> feed
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
	key := fmt.Sprintf("%s/feed.xml", req.PathValue("feed"))
	attrs, err := s.bucket.Attributes(ctx, key)
	blobReader, err := s.bucket.NewReader(ctx, key, nil)
	if err != nil {
		http.Error(w, "Could not access feed", http.StatusInternalServerError)
		log.Printf("construct object reader: %v", err)
		return
	}
	defer blobReader.Close()

	w.Header().Add("Content-Type", "application/xml+rss;charset=UTF-8")
	w.Header().Add("Content-Disposition", "inline")
	w.Header().Add("Cache-Control", "no-cache")
	w.Header().Add("ETag", attrs.ETag)
	http.ServeContent(w, req, "", blobReader.ModTime(), blobReader)
}

func (s *Server) AddEmail(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	feed := req.PathValue("feed")
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

	// ?overwrite disables checking for conflicts
	if req.URL.Query().Get("overwrite") == "" {
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
	}

	item, err := journalclub.FromMessage(msg)
	if err != nil {
		http.Error(w, "Could not parse email", http.StatusBadRequest)
		log.Printf("parse email: %v", err)
		return
	}

	err = s.writeItem(ctx, feed, item)
	if err != nil {
		http.Error(w, "Could not store item", http.StatusBadRequest)
		log.Printf("write item to object store: %v", err)
		return
	}
	err = s.refreshFeed(ctx, feed)
	if err != nil {
		http.Error(w, "Could not refresh feed", http.StatusBadRequest)
		log.Printf("refresh feed: %v", err)
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

func (s *Server) Refresh(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	feed := req.PathValue("feed")

	err := s.refreshFeed(ctx, feed)
	if err != nil {
		http.Error(w, "Could not refresh feed", http.StatusBadRequest)
		log.Printf("refresh feed: %v", err)
		return
	}
}

// WriteItem writes an item to an item file under the feed folder
func (s *Server) writeItem(ctx context.Context, feed string, item *journalclub.Message) error {
	key := fmt.Sprintf("%s/items/%s.json", feed, item.Date.Format(time.RFC3339))

	// Write an item file
	itemWriter, err := s.bucket.NewWriter(ctx, key, &blob.WriterOptions{ContentType: "application/xml+rss;charset=UTF-8"})
	if err != nil {
		return fmt.Errorf("new object writer: %w", err)
	}
	defer itemWriter.Close()
	err = json.NewEncoder(itemWriter).Encode(item)
	if err != nil {
		return fmt.Errorf("write email to items file: %w", err)
	}
	err = itemWriter.Close()
	if err != nil {
		return fmt.Errorf("close items file: %w", err)
	}

	return nil
}

func (s *Server) refreshFeed(ctx context.Context, feed string) error {
	// Read all items file
	var items []journalclub.Message
	iter := s.bucket.List(&blob.ListOptions{Prefix: fmt.Sprintf("%s/items/", feed)})
	for {
		f, err := iter.Next(ctx)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("list items file: %w", err)
		}

		itemReader, err := s.bucket.NewReader(ctx, f.Key, nil)
		if err != nil {
			return fmt.Errorf("construct item object reader: %w", err)
		}
		defer itemReader.Close()

		var item journalclub.Message
		err = json.NewDecoder(itemReader).Decode(&item)
		if err != nil {
			return fmt.Errorf("parse email from item file: %w", err)
		}
		// Prepend item, so that the most recent is first
		items = append(items, item)
		copy(items[1:], items)
		items[0] = item

		err = itemReader.Close()
		if err != nil {
			return fmt.Errorf("close item file: %w", err)
		}
	}

	// Write a new feed file
	feedWriter, err := s.bucket.NewWriter(ctx, fmt.Sprintf("%s/feed.xml", feed), nil)
	if err != nil {
		return fmt.Errorf("new object writer: %w", err)
	}
	defer feedWriter.Close()
	err = s.template.ExecuteTemplate(feedWriter, "feed.xml.tmpl", items)
	if err != nil {
		fmt.Errorf("execute feed template: %w", err)
	}
	err = feedWriter.Close()
	if err != nil {
		return fmt.Errorf("close feed file: %w", err)
	}

	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{feed}/feed.xml", s.GetFeed)
	mux.HandleFunc("POST /{feed}/email", s.AddEmail)
	mux.HandleFunc("POST /{feed}/refresh", s.Refresh)
	mux.ServeHTTP(w, r)
}
