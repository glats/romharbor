package romfetch

import (
	"net"
	"net/http"
	"time"
)

const (
	dialTimeout            = 10 * time.Second
	tlsHandshakeTimeout    = 10 * time.Second
	responseHeaderTimeout  = 30 * time.Second
	idleConnectionLifetime = 90 * time.Second
)

// NewClient returns an HTTP client for outbound provider traffic. A zero
// timeout leaves the whole exchange bounded only by the request context, which
// is what long downloads need; the dial, handshake and response-header phases
// are always bounded.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   tlsHandshakeTimeout,
			ResponseHeaderTimeout: responseHeaderTimeout,
			IdleConnTimeout:       idleConnectionLifetime,
			MaxIdleConns:          20,
			MaxIdleConnsPerHost:   4,
			ForceAttemptHTTP2:     true,
		},
	}
}
