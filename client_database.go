package cbanalytics

import (
	"time"

	"github.com/couchbaselabs/gocbanalytics/internal/httpqueryclient"
)

type databaseClient interface {
	Name() string
	Scope(name string) scopeClient
}

type httpDatabaseClient struct {
	credential                Credential
	client                    *httpqueryclient.Client
	name                      string
	defaultServerQueryTimeout time.Duration
	defaultUnmarshaler        Unmarshaler
	logger                    Logger
}

type httpDatabaseClientConfig struct {
	Credential           Credential
	Client               *httpqueryclient.Client
	Name                 string
	DefaultServerTimeout time.Duration
	DefaultUnmarshaler   Unmarshaler
	Logger               Logger
}

func newHTTPDatabaseClient(cfg httpDatabaseClientConfig) *httpDatabaseClient {
	return &httpDatabaseClient{
		credential:                cfg.Credential,
		client:                    cfg.Client,
		name:                      cfg.Name,
		defaultServerQueryTimeout: cfg.DefaultServerTimeout,
		defaultUnmarshaler:        cfg.DefaultUnmarshaler,
		logger:                    cfg.Logger,
	}
}

func (c *httpDatabaseClient) Name() string {
	return c.name
}

func (c *httpDatabaseClient) Scope(name string) scopeClient {
	return newHTTPScopeClient(httpScopeClientConfig{
		Credential:                c.credential,
		Client:                    c.client,
		DatabaseName:              c.name,
		Name:                      name,
		DefaultServerQueryTimeout: c.defaultServerQueryTimeout,
		DefaultUnmarshaler:        c.defaultUnmarshaler,
		Logger:                    c.logger,
	})
}
