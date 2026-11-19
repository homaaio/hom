package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return FromHTTP(srv.Client(), "hom-test"), srv
}

func TestGetAndStatus(t *testing.T) {
	c, srv := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.Write([]byte("hello"))
			return
		}
		http.NotFound(w, r)
	}))
	r, err := c.Get(context.Background(), srv.URL+"/ok", 100)
	if err != nil || r.Status != 200 || string(r.Body) != "hello" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = c.Get(context.Background(), srv.URL+"/nope", 100)
	if err != nil || r.Status != 404 {
		t.Errorf("a 404 is a status, not an error: %+v %v", r, err)
	}
	if _, err = c.Get(context.Background(), srv.URL+"/ok", 3); !IsNet(err) {
		t.Errorf("an answer over the limit must fail, got %v", err)
	}
	h, err := c.Head(context.Background(), srv.URL+"/ok")
	if err != nil || h.Status != 200 || h.Length != 5 {
		t.Errorf("head: %+v %v", h, err)
	}
}

func TestHTTPSOnly(t *testing.T) {
	c := New("hom-test")
	if _, err := c.Get(context.Background(), "http://example.org/", 10); !IsNet(err) {
		t.Errorf("plain http must be refused, got %v", err)
	}
	if IsHTTPS("http://x") || !IsHTTPS("HTTPS://x") {
		t.Error("IsHTTPS is wrong")
	}
}

func TestRedirectToHTTPRefused(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.org/x", http.StatusFound)
	}))
	defer srv.Close()
	hc := srv.Client()
	hc.CheckRedirect = checkRedirect
	c := FromHTTP(hc, "hom-test")
	if _, err := c.Get(context.Background(), srv.URL+"/", 10); !IsNet(err) {
		t.Errorf("a redirect to http must fail, got %v", err)
	}
}

type counter struct{ last, total int64 }

func (c *counter) Update(received, total int64) { c.last, c.total = received, total }

func TestDownload(t *testing.T) {
	body := []byte(strings.Repeat("0123456789", 20000))
	c, srv := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/f" {
			w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	path := filepath.Join(t.TempDir(), "sub", "f.bin")
	p := &counter{}
	d, err := c.Download(context.Background(), srv.URL+"/f", path, 0, p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if d.Status != 200 || d.Size != int64(len(body)) || d.SHA256 != hex.EncodeToString(sum[:]) || d.Path != path {
		t.Errorf("%+v", d)
	}
	if p.last != int64(len(body)) {
		t.Errorf("progress ended at %d", p.last)
	}
	if b, _ := os.ReadFile(path); len(b) != len(body) {
		t.Error("the file was not written")
	}

	d, err = c.Download(context.Background(), srv.URL+"/missing", filepath.Join(t.TempDir(), "m"), 0, nil)
	if err != nil || d.Status != 404 || d.Path != "" {
		t.Errorf("404: %+v %v", d, err)
	}

	small := filepath.Join(t.TempDir(), "s")
	if _, err = c.Download(context.Background(), srv.URL+"/f", small, 1000, nil); !IsNet(err) {
		t.Errorf("a download over the limit must fail, got %v", err)
	}
	if _, serr := os.Stat(small); serr == nil {
		t.Error("a failed download must not leave a file")
	}
}
