package server

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/cptaffe/email2rss/internal/backend"
	"github.com/cptaffe/email2rss/internal/generic"
	"github.com/cptaffe/email2rss/internal/journalclub"
	"gocloud.dev/blob"
)

const (
	RFC2822 string = "Mon, 02 Jan 2006 15:04:05 MST"
)

type Server struct {
	template *template.Template
	bucket   *blob.Bucket
	backends map[string]backend.Backend
}

// TODO: Abstract the implementation of email -> item state and item states -> feed
func NewServer(templatePath string, bucket *blob.Bucket) (*Server, error) {
	xt := template.New("text").Funcs(template.FuncMap{
		"escape": func(html string) (string, error) {
			var b bytes.Buffer
			err := xml.EscapeText(&b, []byte(html))
			return b.String(), err
		},
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
	return &Server{template: xt, bucket: bucket, backends: map[string]backend.Backend{
		"journalclub": &journalclub.Backend{},
	}}, nil
}

func (s *Server) Backend(feed string) (backend.Backend, error) {
	back, ok := s.backends[feed]
	if !ok {
		return generic.NewBackend(feed), nil
	}
	return back, nil
}

func (s *Server) GetFeed(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	key := fmt.Sprintf("%s/feed.xml", req.PathValue("feed"))
	attrs, err := s.bucket.Attributes(ctx, key)
	if err != nil {
		http.Error(w, "Could not fetch feed attributes", http.StatusInternalServerError)
		log.Printf("fetch object attributes: %v", err)
		return
	}
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
	http.ServeContent(w, req, "feed.xml", blobReader.ModTime(), blobReader)
}

// TODO: serve an item-specific HTML page, possibly the original email or extracted HTML
func (s *Server) GetItem(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	feed := req.PathValue("feed")
	back, err := s.Backend(feed)
	if err != nil {
		http.Error(w, "Could not load backend for feed", http.StatusBadRequest)
		log.Printf("load backend for feed %s: %v", feed, err)
		return
	}

	key := fmt.Sprintf("%s/items/%s.json", req.PathValue("feed"), req.PathValue("key"))
	attrs, err := s.bucket.Attributes(ctx, key)
	if err != nil {
		http.Error(w, "Could not fetch item attributes", http.StatusInternalServerError)
		log.Printf("fetch object attributes: %v", err)
		return
	}
	blobReader, err := s.bucket.NewReader(ctx, key, nil)
	if err != nil {
		http.Error(w, "Could not access item", http.StatusInternalServerError)
		log.Printf("construct object reader: %v", err)
		return
	}
	defer blobReader.Close()

	item, err := back.Decode(blobReader)
	if err != nil {
		http.Error(w, "Could not parse item", http.StatusInternalServerError)
		log.Printf("parse item from file: %v", err)
		return
	}

	switch i := item.(type) {
	case *generic.Message:
		w.Header().Add("Content-Type", "text/html;charset=UTF-8")
		w.Header().Add("Content-Disposition", "inline")
		w.Header().Add("Cache-Control", "no-cache")
		w.Header().Add("ETag", attrs.ETag)
		http.ServeContent(w, req, "item.html", blobReader.ModTime(), strings.NewReader(i.Body))
	default:
		w.Header().Add("Content-Type", "application/json;charset=UTF-8")
		w.Header().Add("Content-Disposition", "inline")
		w.Header().Add("Cache-Control", "no-cache")
		w.Header().Add("ETag", attrs.ETag)
		http.ServeContent(w, req, "item.html", blobReader.ModTime(), blobReader)
	}
}

func (s *Server) AddEmail(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	feed := req.PathValue("feed")
	back, err := s.Backend(feed)
	if err != nil {
		http.Error(w, "Could not load backend for feed", http.StatusBadRequest)
		log.Printf("load backend for feed %s: %v", feed, err)
		return
	}

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

	item, err := back.FromMessage(msg)
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
	err = s.refreshFeed(ctx, back)
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
	back, err := s.Backend(feed)
	if err != nil {
		http.Error(w, "Could not load backend for feed", http.StatusBadRequest)
		log.Printf("load backend for feed %s: %v", feed, err)
		return
	}

	err = s.refreshFeed(ctx, back)
	if err != nil {
		http.Error(w, "Could not refresh feed", http.StatusBadRequest)
		log.Printf("refresh feed: %v", err)
		return
	}
}

// WriteItem writes an item to an item file under the feed folder
func (s *Server) writeItem(ctx context.Context, feed string, item backend.Item) error {
	key := fmt.Sprintf("%s/items/%s.json", feed, item.Key())

	// Write an item file
	itemWriter, err := s.bucket.NewWriter(ctx, key, &blob.WriterOptions{ContentType: "application/json;charset=UTF-8"})
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

type TemplateContext struct {
	Backend backend.Backend
	Items   []backend.Item
}

func (s *Server) refreshFeed(ctx context.Context, back backend.Backend) error {
	// Read all items file
	var items []backend.Item
	iter := s.bucket.List(&blob.ListOptions{Prefix: fmt.Sprintf("%s/items/", back.Name())})
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

		item, err := back.Decode(itemReader)
		if err != nil {
			return fmt.Errorf("parse item from file: %w", err)
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
	feedWriter, err := s.bucket.NewWriter(ctx, fmt.Sprintf("%s/feed.xml", back.Name()), nil)
	if err != nil {
		return fmt.Errorf("new object writer: %w", err)
	}
	defer feedWriter.Close()
	tctx := &TemplateContext{Backend: back, Items: items}
	err = s.template.ExecuteTemplate(feedWriter, back.TemplatePath(), tctx)
	if err != nil {
		return fmt.Errorf("execute feed template: %w", err)
	}
	err = feedWriter.Close()
	if err != nil {
		return fmt.Errorf("close feed file: %w", err)
	}

	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux := http.NewServeMux()
	// special case, backwards compatibility with old system
	mux.HandleFunc("GET /journalclub/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("feed", "journalclub")
		s.GetFeed(w, r)
	})
	mux.HandleFunc("GET /email2rss/{feed}", s.GetFeed)
	mux.HandleFunc("GET /email2rss/{feed}/items/{key}", s.GetItem)
	// TODO: authenticate
	mux.HandleFunc("POST /email2rss/{feed}/email", s.AddEmail)
	mux.HandleFunc("POST /email2rss/{feed}/refresh", s.Refresh)
	mux.ServeHTTP(w, r)
}
