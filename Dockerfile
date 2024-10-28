FROM golang:1.23 AS build

WORKDIR /usr/src/email2rss

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY internal ./internal
COPY main.go main.go
RUN go build -v -o /usr/local/bin/email2rss .

COPY templates ./templates

CMD ["/usr/local/bin/email2rss", "-templates", "/usr/src/email2rss/templates"]
