package lichessbot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	streamNDJSON(path string) (io.ReadCloser, error)
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
	return &httpAPI{token: token, http: &http.Client{}}
}

// NewAPI builds the real lichess client for a Bot, carrying the given
// personal API token on every request.
func NewAPI(token string) api { return newHTTPAPI(token) }

func (a *httpAPI) do(method, url string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
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
		return nil, fmt.Errorf("%s %s: %s: %s", method, url, resp.Status, string(b))
	}
	return resp, nil
}

func (a *httpAPI) streamAt(url string) (io.ReadCloser, error) {
	resp, err := a.do(http.MethodGet, url, nil, "")
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (a *httpAPI) postFormAt(url string, form string) error {
	resp, err := a.do(http.MethodPost, url, strings.NewReader(form), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (a *httpAPI) streamNDJSON(path string) (io.ReadCloser, error) { return a.streamAt(baseURL + path) }

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
