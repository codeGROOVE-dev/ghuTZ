package ghutz

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/codeGROOVE-dev/retry"
)

// httpClient wraps http.Client with retry logic and logging
type httpClient struct {
	client *http.Client
	logger *slog.Logger
}

func newHTTPClient(logger *slog.Logger) *httpClient {
	return &httpClient{
		client: &http.Client{
			Timeout: 60 * time.Second, // Increased from 30s for slow APIs
		},
		logger: logger,
	}
}

// doWithRetry performs HTTP request with exponential backoff and jitter
func (c *httpClient) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	start := time.Now()
	apiName := req.URL.Host
	
	c.logger.Debug("making API request",
		"method", req.Method,
		"url", req.URL.String(),
		"api", apiName,
	)
	
	var resp *http.Response
	var lastErr error
	
	err := retry.Do(
		func() error {
			// Clone request for each retry
			reqCopy := req.Clone(ctx)
			
			var err error
			resp, err = c.client.Do(reqCopy)
			if err != nil {
				c.logger.Warn("API request failed",
					"api", apiName,
					"error", err,
					"duration", time.Since(start),
				)
				lastErr = err
				return err
			}
			
			// Check for rate limiting and server errors
			if resp.StatusCode == http.StatusTooManyRequests {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				c.logger.Warn("rate limited",
					"api", apiName,
					"status", resp.StatusCode,
					"body", string(body),
				)
				lastErr = fmt.Errorf("rate limited by %s", apiName)
				return retry.Unrecoverable(lastErr)
			}
			
			if resp.StatusCode >= 500 {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				c.logger.Warn("server error",
					"api", apiName,
					"status", resp.StatusCode,
					"body", string(body),
				)
				lastErr = fmt.Errorf("server error from %s: %d", apiName, resp.StatusCode)
				return lastErr
			}
			
			// Success or client error (no retry needed)
			c.logger.Debug("API request completed",
				"api", apiName,
				"status", resp.StatusCode,
				"duration", time.Since(start),
			)
			
			return nil
		},
		retry.Attempts(5),
		retry.Delay(time.Second),
		retry.MaxDelay(2*time.Minute),
		retry.DelayType(retry.CombineDelay(retry.BackOffDelay, retry.RandomDelay)),
		retry.OnRetry(func(n uint, err error) {
			c.logger.Info("retrying API request",
				"api", apiName,
				"attempt", n+1,
				"error", err,
			)
		}),
		retry.Context(ctx),
		retry.LastErrorOnly(true),
	)
	
	if err != nil {
		c.logger.Error("API request failed after retries",
			"api", apiName,
			"error", lastErr,
			"duration", time.Since(start),
		)
		return nil, lastErr
	}
	
	return resp, nil
}

// validateURL safely constructs and validates URLs
func validateURL(baseURL string, params map[string]string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	
	return u.String(), nil
}