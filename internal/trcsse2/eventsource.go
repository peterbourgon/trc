package trcsse2

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
)

var (
	// ErrClosed signals that the event source has been closed and will not be
	// reopened.
	ErrClosed = errors.New("closed")

	// ErrInvalidEncoding is returned by Encoder and Decoder when invalid UTF-8
	// event data is encountered.
	ErrInvalidEncoding = errors.New("invalid UTF-8 sequence")
)

// An Event is a message can be written to an event stream and read from an
// event source.
type Event struct {
	Type    string
	ID      string
	Retry   string
	Data    []byte
	ResetID bool
}

// An EventSource consumes server sent events over HTTP with automatic
// recovery.
type EventSource struct {
	retry       time.Duration
	request     *http.Request
	requestBody []byte
	err         error
	r           io.ReadCloser
	dec         *Decoder
	lastEventID string
}

// New prepares an EventSource. The connection is automatically managed, using
// req to connect, and retrying from recoverable errors after waiting the
// provided retry duration.
func New(req *http.Request, retry time.Duration) *EventSource {
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	var reqBody []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			panic(err)
		}
		req.Body.Close()
		req.Body = nil
		reqBody = b
	}

	return &EventSource{
		retry:       retry,
		request:     req,
		requestBody: reqBody,
	}
}

// Close the source. Any further calls to Read() will return ErrClosed.
func (es *EventSource) Close() {
	if es.r != nil {
		es.r.Close()
	}
	es.err = ErrClosed
}

// Connect to an event source, validate the response, and gracefully handle
// reconnects.
func (es *EventSource) connect() {
	for es.err == nil {
		if es.r != nil {
			es.r.Close()
			select {
			case <-time.After(es.retry):
				// ok
			case <-es.request.Context().Done():
				es.err = es.request.Context().Err()
				return
			}
		}

		req := es.request.Clone(es.request.Context())
		if es.requestBody != nil {
			req.Body = io.NopCloser(bytes.NewReader(es.requestBody))
		}

		req.Header.Set("Last-Event-Id", es.lastEventID)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue // reconnect
		}

		switch {
		case resp.StatusCode >= 500:
			resp.Body.Close() // assumed to be temporary, try reconnecting

		case resp.StatusCode == 204:
			resp.Body.Close()
			es.err = ErrClosed

		case resp.StatusCode != 200:
			resp.Body.Close()
			es.err = fmt.Errorf("endpoint returned unrecoverable status %q", resp.Status)

		default:
			mediatype, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
			if mediatype == "text/event-stream" {
				es.r = resp.Body
				es.dec = NewDecoder(es.r)
			} else {
				resp.Body.Close()
				es.err = fmt.Errorf("invalid content type %q", resp.Header.Get("Content-Type"))
			}
			return
		}
	}
}

// Read an event from EventSource. If an error is returned, the EventSource
// will not reconnect, and any further call to Read() will return the same
// error.
func (es *EventSource) Read() (Event, error) {
	if es.r == nil {
		es.connect()
	}

	for es.err == nil {
		var e Event

		err := es.dec.Decode(&e)

		if err == ErrInvalidEncoding {
			continue
		}

		if err != nil {
			es.connect()
			continue
		}

		if len(e.Data) == 0 {
			continue
		}

		if len(e.ID) > 0 || e.ResetID {
			es.lastEventID = e.ID
		}

		if len(e.Retry) > 0 {
			if retry, err := strconv.Atoi(e.Retry); err == nil {
				es.retry = time.Duration(retry) * time.Millisecond
			}
		}

		return e, nil
	}

	return Event{}, es.err
}
