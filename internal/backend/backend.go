// Backend provides the interface between email2rss and each backend e.g. journalclub
package backend

import (
	"io"
	"net/mail"
)

type Item interface {
	Key() string
	Encode(w io.Writer) error
}

type Backend interface {
	Name() string
	TemplatePath() string
	FromMessage(*mail.Message) (Item, error)
	Decode(r io.Reader) (Item, error)
}
