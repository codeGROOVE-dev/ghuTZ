package ghutz

import (
	"context"
	"net/http"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/httpcache"
)

// CachedHTTPDo performs an HTTP request with caching support.
func (d *Detector) cachedHTTPDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	// Create a cached HTTP client using the detector's cache and retryable HTTP client
	cachedClient := httpcache.NewCachedHTTPClient(d.cache, &retryableHTTPClient{
		detector: d,
		doFunc:   func(req *http.Request) (*http.Response, error) { return d.retryableHTTPDo(ctx, req) },
	}, d.logger)
	return cachedClient.Do(ctx, req)
}

// retryableHTTPClient wraps the Detector's retryableHTTPDo method to implement HTTPClient interface
type retryableHTTPClient struct {
	detector *Detector
	doFunc   func(*http.Request) (*http.Response, error)
}

func (r *retryableHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return r.doFunc(req)
}
