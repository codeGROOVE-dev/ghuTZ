package ghutz

import (
	"context"
	"net/http"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/httpcache"
)

// CachedHTTPDo performs an HTTP request with caching support.
func (d *Detector) cachedHTTPDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	// Create a cached HTTP client using the detector's cache and retryable HTTP client
	cachedClient := httpcache.NewCachedHTTPClient(d.cache, &retryableHTTPClient{detector: d, ctx: ctx}, d.logger)
	return cachedClient.Do(ctx, req)
}

// retryableHTTPClient wraps the Detector's retryableHTTPDo method to implement HTTPClient interface
type retryableHTTPClient struct {
	detector *Detector
	ctx      context.Context
}

func (r *retryableHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return r.detector.retryableHTTPDo(r.ctx, req)
}