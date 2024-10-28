package email

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
)

// MessageMIME finds and parses a portion of the message based on the MIME type
func MessageMIME(message *mail.Message, contentType string) (io.Reader, error) {
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
