package cbanalytics

import (
	"time"

	"github.com/couchbaselabs/gocbanalytics/internal/httpqueryclient"
)

type scopeClient interface {
	Name() string
	QueryClient() queryClient
}

type httpScopeClient struct {
	credential                Credential
	client                    *httpqueryclient.Client
	name                      string
	databaseName              string
	defaultServerQueryTimeout time.Duration
	defaultUnmarshaler        Unmarshaler
	logger                    Logger
}

type httpScopeClientConfig struct {
	Credential                Credential
	Client                    *httpqueryclient.Client
	DatabaseName              string
	Name                      string
	DefaultServerQueryTimeout time.Duration
	DefaultUnmarshaler        Unmarshaler
	Logger                    Logger
}

func newHTTPScopeClient(cfg httpScopeClientConfig) *httpScopeClient {
	return &httpScopeClient{
		credential:                cfg.Credential,
		client:                    cfg.Client,
		name:                      cfg.Name,
		databaseName:              cfg.DatabaseName,
		defaultServerQueryTimeout: cfg.DefaultServerQueryTimeout,
		defaultUnmarshaler:        cfg.DefaultUnmarshaler,
		logger:                    cfg.Logger,
	}
}

func (c *httpScopeClient) Name() string {
	return c.name
}

func (c *httpScopeClient) QueryClient() queryClient {
	return newHTTPQueryClient(httpQueryClientConfig{
		Credential:                c.credential,
		Client:                    c.client,
		DefaultServerQueryTimeout: c.defaultServerQueryTimeout,
		DefaultUnmarshaler:        c.defaultUnmarshaler,
		Namespace: &queryClientNamespace{
			Database: c.databaseName,
			Scope:    c.name,
		},
		Logger: c.logger,
	})
}
