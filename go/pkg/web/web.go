// Package web is hom's HTTP client: https only, streaming downloads with a hash made on the way.
package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NetError is a failure of the connection or of the server's answer (spec exit code 4).
// Any other error from this package is a local one (disk, permissions).
type NetError struct{ Msg string }

func (e *NetError) Error() string { return e.Msg }

func netErr(format string, a ...any) error { return &NetError{Msg: fmt.Sprintf(format, a...)} }

// IsNet reports whether err is a NetError.
func IsNet(err error) bool {
	var ne *NetError
	return errors.As(err, &ne)
}

// Client makes the requests. Proxy settings come from the environment (HTTPS_PROXY).
type Client struct {
	HTTP *http.Client
	UA   string
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if !strings.EqualFold(req.URL.Scheme, "https") {
		return errors.New("a redirect to a non-https address was refused")
	}
	return nil
}

// New makes a client with timeouts that suit a slow link: a long body is fine, a silent server is not.
func New(ua string) *Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 40 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true, // a package is saved exactly as the server holds it
	}
	return &Client{HTTP: &http.Client{Transport: tr, CheckRedirect: checkRedirect}, UA: ua}
}

// FromHTTP wraps a ready http.Client (tests use one that trusts their server's certificate).
func FromHTTP(h *http.Client, ua string) *Client { return &Client{HTTP: h, UA: ua} }

// IsHTTPS reports whether s starts with https://.
func IsHTTPS(s string) bool {
	return len(s) >= 8 && strings.EqualFold(s[:8], "https://")
}

func (c *Client) do(ctx context.Context, method, url string) (*http.Response, error) {
	if !IsHTTPS(url) {
		return nil, netErr("only https:// addresses are allowed: %s", url)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, netErr("bad address: %v", err)
	}
	req.Header.Set("User-Agent", c.UA)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, netErr("%v", err)
	}
	return resp, nil
}

// Result is the answer to a Get or a Head.
type Result struct {
	Status int
	Body   []byte // Get, status 200 only
	Length int64  // Content-Length, -1 if unknown
}

// Get fetches a small document into memory (at most max bytes). A non-200 status is not an
// error here: the caller decides. Only a failed connection or a cut-off body is.
func (c *Client) Get(ctx context.Context, url string, max int64) (*Result, error) {
	resp, err := c.do(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	r := &Result{Status: resp.StatusCode, Length: resp.ContentLength}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return r, nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, netErr("the connection dropped: %v", err)
	}
	if int64(len(b)) > max {
		return nil, netErr("the answer is larger than %d bytes", max)
	}
	if resp.ContentLength >= 0 && int64(len(b)) != resp.ContentLength {
		return nil, netErr("the answer was cut short")
	}
	r.Body = b
	return r, nil
}

// GetRetry is Get, tried up to three times when the connection fails or the server answers 5xx.
func (c *Client) GetRetry(ctx context.Context, url string, max int64) (*Result, error) {
	var r *Result
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		r, err = c.Get(ctx, url, max)
		if err == nil && r.Status < 500 {
			return r, nil
		}
		if ctx.Err() != nil || attempt == 3 {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return r, err
}

// Head asks for the headers only.
func (c *Client) Head(ctx context.Context, url string) (*Result, error) {
	resp, err := c.do(ctx, http.MethodHead, url)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	return &Result{Status: resp.StatusCode, Length: resp.ContentLength}, nil
}

// Progress is told how much has arrived (total is -1 when the server did not say).
type Progress interface {
	Update(received, total int64)
}

// Download is the outcome of a Download call.
type Download struct {
	Status int    // anything but 200: no file was written
	Size   int64  // bytes written
	SHA256 string // lowercase hex, made while writing
	Path   string // the file that was written
}

// Download streams a URL into the file at path and hashes it on the way. Status != 200 gives
// a result without a file. On any error the file is removed. At most max bytes (0 = no limit).
func (c *Client) Download(ctx context.Context, url, path string, max int64, p Progress) (*Download, error) {
	resp, err := c.do(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	d := &Download{Status: resp.StatusCode}
	if resp.StatusCode != http.StatusOK {
		return d, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*Download, error) {
		f.Close()
		os.Remove(path)
		return nil, e
	}
	h := sha256.New()
	total := resp.ContentLength
	if p != nil {
		p.Update(0, total)
	}
	buf := make([]byte, 64*1024)
	var n int64
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, werr := f.Write(buf[:k]); werr != nil {
				return fail(werr)
			}
			h.Write(buf[:k])
			n += int64(k)
			if max > 0 && n > max {
				return fail(netErr("the download is larger than the limit of %d bytes", max))
			}
			if p != nil {
				p.Update(n, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fail(netErr("the connection dropped: %v", rerr))
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	if total >= 0 && n != total {
		os.Remove(path)
		return nil, netErr("the download was cut short (%d of %d bytes)", n, total)
	}
	if n == 0 {
		os.Remove(path)
		return nil, netErr("the server sent an empty file")
	}
	d.Size = n
	d.SHA256 = hex.EncodeToString(h.Sum(nil))
	d.Path = path
	return d, nil
}
