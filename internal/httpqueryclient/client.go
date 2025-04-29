// Package httpqueryclient implements an HTTP client for making requests against a server.
package httpqueryclient

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/couchbaselabs/gocbanalytics/internal/logging"
)

// ClientConfig holds the configuration for the client.
type ClientConfig struct {
	TLSConfig *tls.Config
	Logger    logging.Logger
}

// Client represents an HTTP client that can be used to make requests to the server.
type Client struct {
	scheme      string
	host        string
	port        int
	innerClient *http.Client
	resolver    *net.Resolver
	logger      logging.Logger
}

// NewClient creates a new Client with the given endpoint and configuration.
func NewClient(scheme string, host string, port int, config ClientConfig) *Client {
	client, resolver := createHTTPClient(config.TLSConfig)

	return &Client{
		scheme:      scheme,
		host:        host,
		port:        port,
		innerClient: client,
		resolver:    resolver,
		logger:      config.Logger,
	}
}

// Close closes the client and releases any resources it holds.
func (c *Client) Close() error {
	if tsport, ok := c.innerClient.Transport.(*http.Transport); ok {
		tsport.CloseIdleConnections()
	}

	return nil
}

func createHTTPClient(tlsConfig *tls.Config) (*http.Client, *net.Resolver) {
	resolver := net.DefaultResolver

	httpDialer := &net.Dialer{ //nolint:exhaustruct
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver:  resolver,
	}

	// We set ForceAttemptHTTP2, which will update the base-config to support HTTP2
	// automatically, so that all configs from it will look for that.
	httpTransport := &http.Transport{ //nolint:exhaustruct
		ForceAttemptHTTP2: true,

		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return httpDialer.DialContext(ctx, network, addr)
		},

		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        0,
		MaxIdleConnsPerHost: 0,
		MaxConnsPerHost:     0,
		IdleConnTimeout:     1000 * time.Millisecond,
	}

	httpCli := &http.Client{ //nolint:exhaustruct
		Transport: httpTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// All that we're doing here is setting auth on any redirects.
			// For that reason we can just pull it off the oldest (first) request.
			if len(via) >= 10 {
				// Just duplicate the default behaviour for maximum redirects.
				return errors.New("stopped after 10 redirects") //nolint:err113
			}

			oldest := via[0]
			auth := oldest.Header.Get("Authorization")
			if auth != "" {
				req.Header.Set("Authorization", auth)
			}

			return nil
		},
	}

	return httpCli, resolver
}
