package lichessbot

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The real client, exercised against a local server standing in for
// lichess: streamNDJSON must reach the given path with the bearer token,
// and postForm must do the same for a write.
func TestHTTPAPIStreamNDJSONReachesThePathWithTheToken(t *testing.T) {
	var gotPath, gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/stream/event", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"type":"gameStart","game":{"id":"g1"}}` + "\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := &httpAPI{token: "tok", http: srv.Client()}
	body, err := a.streamAt(srv.URL + "/api/stream/event")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()

	if gotPath != "/api/stream/event" {
		t.Errorf("got path %q", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("got Authorization %q", gotAuth)
	}
}

func TestHTTPAPIStreamNDJSONErrorsOnANon2xxResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	a := &httpAPI{token: "bad", http: srv.Client()}
	if _, err := a.streamAt(srv.URL + "/anything"); err == nil {
		t.Error("a 401 was not reported as an error")
	}
}

func TestHTTPAPIPostFormReachesThePathWithTheBodyAndToken(t *testing.T) {
	var gotAuth, gotBody, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
	}))
	defer srv.Close()

	a := &httpAPI{token: "tok", http: srv.Client()}
	if err := a.postFormAt(srv.URL, "offeringDraw=true"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("got Authorization %q", gotAuth)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("got Content-Type %q", gotContentType)
	}
	if gotBody != "offeringDraw=true" {
		t.Errorf("got body %q", gotBody)
	}
}

// NewAPI must hand back something that actually behaves as the api
// interface Bot uses; a wrong token here fails at the first request, not
// at construction, so the only thing to check at this level is the type.
func TestNewAPIReturnsAUsableClient(t *testing.T) {
	var _ api = NewAPI("any-token")
}

// do() itself: a bad URL must be reported rather than panicking, and a
// successful GET's body must reach the caller intact.
func TestHTTPAPIDoReturnsTheBodyOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer srv.Close()
	a := &httpAPI{token: "t", http: srv.Client()}
	resp, err := a.do(http.MethodGet, srv.URL+"/x", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 16)
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != "hello" {
		t.Errorf("got body %q", string(buf[:n]))
	}
}

func TestHTTPAPIDoRejectsAMalformedRequest(t *testing.T) {
	a := &httpAPI{token: "t", http: http.DefaultClient}
	if _, err := a.do("BAD METHOD\n", "http://x", nil, ""); err == nil {
		t.Error("a malformed method was accepted")
	}
}

// The path-relative wrappers streamNDJSON and postForm are what Bot
// actually calls; this proves they reach baseURL+path, not just that the
// absolute-URL helpers underneath them work.
func TestStreamNDJSONAndPostFormUseTheBaseURL(t *testing.T) {
	var gotPaths []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/stream/event", func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		w.Write([]byte("{}\n"))
	})
	mux.HandleFunc("/api/challenge/c1/accept", func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	old := baseURL
	baseURL = srv.URL
	defer func() { baseURL = old }()

	a := newHTTPAPI("tok")
	a.http = srv.Client()

	body, err := a.streamNDJSON("/api/stream/event")
	if err != nil {
		t.Fatal(err)
	}
	body.Close()
	if err := a.postForm("/api/challenge/c1/accept", ""); err != nil {
		t.Fatal(err)
	}
	if len(gotPaths) != 2 || gotPaths[0] != "/api/stream/event" || gotPaths[1] != "/api/challenge/c1/accept" {
		t.Errorf("got %v", gotPaths)
	}
}

// A transport-level failure (nothing listening on the other end) must
// come back as an error too, not just a bad-request or non-2xx response.
func TestHTTPAPIDoReturnsATransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening here any more
	a := &httpAPI{token: "t", http: http.DefaultClient}
	if _, err := a.do(http.MethodGet, url, nil, ""); err == nil {
		t.Error("a connection to a closed server was not reported as an error")
	}
}
