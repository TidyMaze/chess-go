package lichessbot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// baseURL is a var, not a const, so a test can point streamNDJSON and
// postForm at a local server instead of the real lichess.org.
var baseURL = "https://lichess.org"

// api is the network surface this package needs from lichess, kept small
// and behind an interface so the game loop can be tested against a fake
// server instead of the real one. streamNDJSON returns the response body
// of a newline-delimited-JSON GET; postForm posts an application/x-www
// -form-urlencoded body and returns an error unless lichess answers 2xx.
type api interface {
	streamNDJSON(ctx context.Context, path string) (io.ReadCloser, error)
	postForm(path string, form string) error
}

// httpAPI is the real client: every request carries the bot's own token,
// read once at startup and never logged, never written to a file, never
// echoed back on any response path. do, streamAt and postFormAt all take
// an absolute URL rather than a lichess-relative path, so a test can point
// them at a local server; streamNDJSON and postForm are the path-relative
// wrappers the api interface and production code use.
type httpAPI struct {
	token string
	http  *http.Client
}

func newHTTPAPI(token string) *httpAPI {
	// No Client.Timeout: it caps the whole exchange, body included, which
	// would cut every stream off at the deadline. The timeouts that matter
	// for a stream live on the transport and only cover getting connected
	// and answered, never how long the stream then runs.
	return &httpAPI{token: token, http: &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			ForceAttemptHTTP2:     true,
			// This is what notices a dead connection, and it has to be the
			// transport rather than anything counting silence up here: an
			// event stream with nothing happening on it sends no keepalives
			// at all. Measured against lichess on 2026-09-13, an idle event
			// stream delivered zero bytes in 75 seconds while the HTTP/2
			// connection stayed open and healthy, so any rule that treats
			// silence as death would reconnect the bot forever for nothing.
			// A ping asks the connection instead of guessing from silence.
			HTTP2: &http.HTTP2Config{
				SendPingTimeout: 30 * time.Second,
				PingTimeout:     15 * time.Second,
			},
		},
	}}
}

// NewAPI builds the real lichess client for a Bot, carrying the given
// personal API token on every request.
func NewAPI(token string) api { return newHTTPAPI(token) }

func (a *httpAPI) do(ctx context.Context, method, url string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &statusError{code: resp.StatusCode, msg: fmt.Sprintf("%s %s: %s: %s", method, url, resp.Status, string(b))}
	}
	return resp, nil
}

// statusError is lichess answering, as opposed to lichess not being reached.
// The difference decides whether trying again can help: a 400 on a move or a
// 404 on a game says the same thing next time, a dropped connection or a 502
// while lichess restarts does not.
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

// isDefinitive reports whether err is lichess refusing the request itself (a
// 4xx other than 429), which no retry will change.
func isDefinitive(err error) bool {
	var se *statusError
	return errors.As(err, &se) && se.code >= 400 && se.code < 500 && se.code != http.StatusTooManyRequests
}

// isRateLimited reports whether err is lichess answering 429: the token is
// over a rate limit, and lichess asks for a minute's pause before the next
// request (lichess-api.yaml, "Rate limiting").
func isRateLimited(err error) bool {
	var se *statusError
	return errors.As(err, &se) && se.code == http.StatusTooManyRequests
}

func (a *httpAPI) streamAt(ctx context.Context, url string) (io.ReadCloser, error) {
	resp, err := a.do(ctx, http.MethodGet, url, nil, "")
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (a *httpAPI) postFormAt(url string, form string) error {
	// A move post that never returns would block its game for good, so it
	// gets a deadline of its own. Generous: losing a move to a slow network
	// is worse than waiting for it.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := a.do(ctx, http.MethodPost, url, strings.NewReader(form), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (a *httpAPI) streamNDJSON(ctx context.Context, path string) (io.ReadCloser, error) {
	return a.streamAt(ctx, baseURL+path)
}

func (a *httpAPI) postForm(path string, form string) error { return a.postFormAt(baseURL+path, form) }

// eachLine decodes each line of a newline-delimited-JSON stream into dst,
// skipping blank keep-alive lines lichess sends between events, and calls
// handle for every successfully decoded one. It returns when the stream
// ends (the game or the event feed closed) or handle returns an error.
func eachLine(r io.Reader, handle func(line []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := handle(line); err != nil {
			return err
		}
	}
	return sc.Err()
}

// eventKind is the minimal shape every event on both streams shares: the
// "type" field that says how to decode the rest.
type eventKind struct {
	Type string `json:"type"`
}

func parseKind(line []byte) (string, error) {
	var k eventKind
	if err := json.Unmarshal(line, &k); err != nil {
		return "", err
	}
	return k.Type, nil
}
