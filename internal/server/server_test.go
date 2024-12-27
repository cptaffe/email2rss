package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "embed"

	"github.com/cptaffe/email2rss/internal/generic"
	"github.com/cptaffe/email2rss/internal/journalclub"
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/memblob"
)

//go:embed test/email.rfc822
var testEmail string

//go:embed test/email.html
var testHTML string

func TestAddJournalClubEmail(t *testing.T) {
	ctx := context.Background()
	bucket, err := blob.OpenBucket(ctx, "mem://")
	if err != nil {
		t.Fatalf("open bucket: %v", err)
	}
	defer bucket.Close()

	s, err := NewServer("../../templates", bucket)
	if err != nil {
		t.Fatalf("construct server: %v", err)
	}

	rec := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/journalclub/email", strings.NewReader(testEmail))
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	req.SetPathValue("feed", "journalclub")
	s.AddEmail(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status is %d, expected 201", rec.Code)
	}

	var resp journalclub.Message
	err = json.NewDecoder(rec.Body).Decode(&resp)
	if err != nil {
		t.Errorf("deserialize body into email: %v", err)
	}

	timestamp := "2024-10-21T12:45:12Z"
	expectedDate, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Errorf("parse date: %v", err)
	}
	expected := journalclub.Message{
		UUID:        "4489904c-91ae-4fbf-b4e7-915007267da1",
		Subject:     "A Scalable Real-Time SDN-Based MQTT Framework for Industrial Applications",
		Description: "Today's article comes from the IEEE Open Journal of the Industrial Electronics Society. The authors are Shahri et al., from the University of Aveiro, in Portugal. In this paper they argue that the MQTT protocol is not suitable for industrial applications because it lacks timeliness guarantees. They propose a new system to overcome these limitations. Let's see what they came up with.",
		Date:        expectedDate,
		ImageURL:    "https://embed.filekitcdn.com/e/3Uk7tL4uX5yjQZM3sj7FA5/gyTk6Miin8sMsEFuV8waDs",
		AudioURL:    "https://s3.amazonaws.com/journalclub.io/mqtt-full.mp3",
		AudioSize:   18218972,
		PaperURL:    "https://doi.org/10.1109/OJIES.2024.3373232",
	}

	if resp != expected {
		t.Errorf("response does not match expected value:\nhave:    %v\nexpected:%v", resp, expected)
	}

	key := fmt.Sprintf("journalclub/items/%s.json", timestamp)
	itemReader, err := bucket.NewReader(ctx, key, nil)
	if err != nil {
		t.Fatalf("failed to read from bucket: %v", err)
	}
	var stored journalclub.Message
	err = json.NewDecoder(itemReader).Decode(&stored)
	if err != nil {
		t.Errorf("deserialize body into email: %v", err)
	}

	if resp != expected {
		t.Errorf("stored does not match expected value:\nhave:    %v\nexpected:%v", resp, expected)
	}

	ok, err := bucket.Exists(ctx, "journalclub/feed.xml")
	if err != nil {
		t.Fatalf("failed to read from bucket: %v", err)
	}
	if !ok {
		t.Error("Expected a new journalclub/feed.xml file")
	}
}

func TestAddEmail(t *testing.T) {
	ctx := context.Background()
	bucket, err := blob.OpenBucket(ctx, "mem://")
	if err != nil {
		t.Fatalf("open bucket: %v", err)
	}
	defer bucket.Close()

	s, err := NewServer("../../templates", bucket)
	if err != nil {
		t.Fatalf("construct server: %v", err)
	}

	rec := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "email2rss/test/email", strings.NewReader(testEmail))
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	req.SetPathValue("feed", "test")
	s.AddEmail(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status is %d, expected 201", rec.Code)
	}

	var resp generic.Message
	err = json.NewDecoder(rec.Body).Decode(&resp)
	if err != nil {
		t.Errorf("deserialize body into email: %v", err)
	}

	timestamp := "2024-10-21T12:45:12Z"
	expectedDate, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Errorf("parse date: %v", err)
	}
	expected := generic.Message{
		UUID:    "4489904c-91ae-4fbf-b4e7-915007267da1",
		Subject: "A Scalable Real-Time SDN-Based MQTT Framework for Industrial Applications",
		Date:    expectedDate,
		Body:    testHTML,
	}

	if resp != expected {
		t.Errorf("response does not match expected value:\nhave:    %v\nexpected:%v", resp, expected)
	}

	key := fmt.Sprintf("test/items/%s.json", timestamp)
	itemReader, err := bucket.NewReader(ctx, key, nil)
	if err != nil {
		t.Fatalf("failed to read from bucket: %v", err)
	}
	var stored generic.Message
	err = json.NewDecoder(itemReader).Decode(&stored)
	if err != nil {
		t.Errorf("deserialize body into email: %v", err)
	}

	if resp != expected {
		t.Errorf("stored does not match expected value:\nhave:    %v\nexpected:%v", resp, expected)
	}

	ok, err := bucket.Exists(ctx, "test/feed.xml")
	if err != nil {
		t.Fatalf("failed to read from bucket: %v", err)
	}
	if !ok {
		t.Error("Expected a new test/feed.xml file")
	}
}
