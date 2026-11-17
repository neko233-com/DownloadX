// Package httpx builds HTTP clients and applies task headers consistently.
package httpx

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/version"
)

// NewClient builds a client tuned for many concurrent range requests.
func NewClient(t *engine.Task) (*http.Client, error) {
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	n := t.ConnectionCount()

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		MaxIdleConns:          2 * n,
		MaxIdleConnsPerHost:   n + 1,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
	}

	if t.Proxy != "" {
		u, err := url.Parse(t.Proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy %q: %w", t.Proxy, err)
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, fmt.Errorf("unsupported proxy scheme %q (want http, https, socks5)", u.Scheme)
		}
		transport.Proxy = http.ProxyURL(u)
	}

	if t.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &http.Client{Transport: transport}, nil
}

// ApplyHeaders sets user-agent, cookies, referer, custom headers and
// credentials on req. Credentials embedded in the URL take precedence.
func ApplyHeaders(req *http.Request, t *engine.Task) {
	ua := t.UserAgent
	if ua == "" {
		ua = "DownloadX/" + version.Version
	}
	req.Header.Set("User-Agent", ua)
	if t.Cookie != "" {
		req.Header.Set("Cookie", t.Cookie)
	}
	if t.Referer != "" {
		req.Header.Set("Referer", t.Referer)
	}
	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}
	if u := req.URL.User; u != nil {
		pass, _ := u.Password()
		req.SetBasicAuth(u.Username(), pass)
	} else if t.Username != "" {
		req.SetBasicAuth(t.Username, t.Password)
	}
}
