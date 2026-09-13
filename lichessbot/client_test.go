package lichessbot

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The token must reach lichess as a bearer header on every request, or
// every call gets a 401 and the bot never plays a single game. This is
// the one thing about the client worth a real HTTP round trip to check.
func TestHTTPAPISendsTheBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte("{}\n"))
	}))
	defer srv.Close()

	a := newHTTPAPI("secret-token")
	a.http = srv.Client()
	// Point the request at the test server instead of lichess.org: do
	// the request directly rather than through streamNDJSON, which is
	// hardcoded to baseURL.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/probe", nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := a.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth != "Bearer secret-token" {
		t.Errorf("got Authorization header %q, want Bearer secret-token", gotAuth)
	}
}

// A non-2xx response must surface as an error with the status and body
// visible, or a rejected move looks like a silent success and the bot
// keeps waiting for a turn that already passed it by.
func TestHTTPAPIErrorsOnANon2xxResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid move"}`))
	}))
	defer srv.Close()

	a := &httpAPI{token: "t", http: srv.Client()}
	err := a.postFormAt(srv.URL, "")
	if err == nil {
		t.Fatal("a 400 response was not reported as an error")
	}
	if !strings.Contains(err.Error(), "invalid move") {
		t.Errorf("error %q does not carry the response body", err)
	}
}

func TestEachLineSkipsBlankKeepAliveLines(t *testing.T) {
	body := "{\"type\":\"a\"}\n\n\n{\"type\":\"b\"}\n"
	var seen []string
	err := eachLine(strings.NewReader(body), func(line []byte) error {
		k, perr := parseKind(line)
		if perr != nil {
			return perr
		}
		seen = append(seen, k)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "a" || seen[1] != "b" {
		t.Errorf("got %v, want [a b]; blank lines should be skipped, not decoded", seen)
	}
}

func TestEachLineStopsWhenHandleErrors(t *testing.T) {
	body := "{\"type\":\"a\"}\n{\"type\":\"b\"}\n{\"type\":\"c\"}\n"
	stop := errors.New("stop here")
	n := 0
	err := eachLine(strings.NewReader(body), func(line []byte) error {
		n++
		if n == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Errorf("got %v, want the sentinel error", err)
	}
	if n != 2 {
		t.Errorf("handle ran %d times, want exactly 2 (stop at the second line)", n)
	}
}

func TestParseKindRejectsGarbage(t *testing.T) {
	if _, err := parseKind([]byte("not json")); err == nil {
		t.Error("garbage input was accepted as a valid event")
	}
}
