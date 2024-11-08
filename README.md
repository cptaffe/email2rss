# email2rss

A service for generating an RSS feed from emails, specifically the JournalClub newsletter.

## API

The `POST /{feed}/email` endpoint accepts a raw email and updates the RSS feed with the new information:

```sh
; curl \
    --silent \
    --show-error \
    --fail-with-body \
    --header "Content-Type: message/rfc822" \
    --header "Accept: application/json" \
    --data-binary @- \
    'http://email2rss.default.svc.k8s.home.arpa/journalclub/email' \
    < email.eml
{"uuid":"1b1dd75f-e37e-4c55-b759-dea3b1dbba3a","subject":"Employing deep learning in crisis management and decision making through prediction using time series data in Mosul Dam Northern Iraq","description":"Today's article comes from the PeerJ Computer Science journal. The authors are Khafaji et al., from the University of Sfax, in Tunisia. In this paper they attempt to develop machine learning models that can predict the water-level fluctuations within a dam in Iraq. If they succeed, it will help the dam operators prevent a catastrophic collapse. Let's see how well they did.","date":"2024-11-03T13:55:35Z","imageURL":"https://embed.filekitcdn.com/e/3Uk7tL4uX5yjQZM3sj7FA5/sSM8ecFNXywfm7M3qy1tWu","audioURL":"REDACTED","audioSize":12926609,"paperURL":"http://dx.doi.org/10.7717/peerj-cs.2416"}
```

If `?overwrite` is set, the item is updated even if there's already an item for that timestamp.

The `GET /{feed}/feed.xml` endpoint provides the full RSS feed, for use in a Podcasts app:

```sh
; curl \
    --silent \
    --show-error \
    --fail-with-body \
    http://email2rss.default.svc.k8s.home.arpa/journalclub/feed.xml \
    | head -n3
<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"
  xmlns:atom="http://www.w3.org/2005/Atom"
```

## Tools

The `email2jc` tool takes an raw email (such as exported from a mail client) as input, and outputs the state file which would be used to generate one `<item>` in a feed:

```sh
; email2jc < email.eml
{"uuid":"1b1dd75f-e37e-4c55-b759-dea3b1dbba3a","subject":"Employing deep learning in crisis management and decision making through prediction using time series data in Mosul Dam Northern Iraq","description":"Today's article comes from the PeerJ Computer Science journal. The authors are Khafaji et al., from the University of Sfax, in Tunisia. In this paper they attempt to develop machine learning models that can predict the water-level fluctuations within a dam in Iraq. If they succeed, it will help the dam operators prevent a catastrophic collapse. Let's see how well they did.","date":"2024-11-03T13:55:35Z","imageURL":"https://embed.filekitcdn.com/e/3Uk7tL4uX5yjQZM3sj7FA5/sSM8ecFNXywfm7M3qy1tWu","audioURL":"REDACTED","audioSize":12926609,"paperURL":"http://dx.doi.org/10.7717/peerj-cs.2416"}
```

The `email2html` tool takes a raw email and outputs the decoded HTML portion of the email's body:

```sh
; email2html < email.eml | head -n3
<!DOCTYPE html>
<html style="line-height:1.5">
<head>
```
