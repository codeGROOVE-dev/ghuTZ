package ghutz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CacheEntry struct {
	Data      []byte    `json:"data"`
	ExpiresAt time.Time `json:"expires_at"`
	ETag      string    `json:"etag,omitempty"`
}

type DiskCache struct {
	dir    string
	ttl    time.Duration
	logger *slog.Logger
}

func NewDiskCache(dir string, ttl time.Duration, logger *slog.Logger) (*DiskCache, error) {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}
	
	return &DiskCache{
		dir:    dir,
		ttl:    ttl,
		logger: logger,
	}, nil
}

func (c *DiskCache) getCacheKey(url string) string {
	h := sha256.New()
	h.Write([]byte(url))
	return hex.EncodeToString(h.Sum(nil))
}

// getCacheKeyForAPICall generates a cache key for API calls including request body
func (c *DiskCache) getCacheKeyForAPICall(url string, requestBody []byte) string {
	h := sha256.New()
	h.Write([]byte(url))
	h.Write(requestBody)
	return hex.EncodeToString(h.Sum(nil))
}

func (c *DiskCache) getCachePath(key string) string {
	// Use subdirectories to avoid too many files in one directory
	return filepath.Join(c.dir, key[:2], key[2:4], key+".json")
}

func (c *DiskCache) Get(url string) ([]byte, string, bool) {
	key := c.getCacheKey(url)
	path := c.getCachePath(key)
	
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			c.logger.Warn("CACHE MISS - file not found", "url", url)
		} else {
			c.logger.Error("CACHE MISS - read error", "url", url, "error", err)
		}
		return nil, "", false
	}
	
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		c.logger.Error("CACHE MISS - unmarshal error", "url", url, "error", err)
		return nil, "", false
	}
	
	// Check if expired
	if time.Now().After(entry.ExpiresAt) {
		c.logger.Warn("CACHE MISS - expired", "url", url, "expired_at", entry.ExpiresAt)
		return nil, "", false
	}
	
	// No log for cache hit to reduce noise
	return entry.Data, entry.ETag, true
}

func (c *DiskCache) Set(url string, data []byte, etag string) error {
	key := c.getCacheKey(url)
	path := c.getCachePath(key)
	
	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}
	
	entry := CacheEntry{
		Data:      data,
		ExpiresAt: time.Now().Add(c.ttl),
		ETag:      etag,
	}
	
	jsonData, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling cache entry: %w", err)
	}
	
	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		return fmt.Errorf("writing cache file: %w", err)
	}
	
	c.logger.Debug("cache set", "url", url, "expires_at", entry.ExpiresAt)
	return nil
}

// SetAPICall caches an API call response with request body considered in the key
func (c *DiskCache) SetAPICall(url string, requestBody []byte, data []byte) error {
	key := c.getCacheKeyForAPICall(url, requestBody)
	path := c.getCachePath(key)
	
	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}
	
	entry := CacheEntry{
		Data:      data,
		ExpiresAt: time.Now().Add(c.ttl),
		ETag:      "", // API calls don't typically use ETags
	}
	
	jsonData, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling cache entry: %w", err)
	}
	
	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		return fmt.Errorf("writing cache file: %w", err)
	}
	
	c.logger.Debug("API cache set", "url", url, "expires_at", entry.ExpiresAt)
	return nil
}

// GetAPICall retrieves a cached API call response
func (c *DiskCache) GetAPICall(url string, requestBody []byte) ([]byte, bool) {
	key := c.getCacheKeyForAPICall(url, requestBody)
	path := c.getCachePath(key)
	
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			c.logger.Warn("API CACHE MISS - file not found", "url", url)
		} else {
			c.logger.Error("API CACHE MISS - read error", "url", url, "error", err)
		}
		return nil, false
	}
	
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		c.logger.Error("API CACHE MISS - unmarshal error", "url", url, "error", err)
		return nil, false
	}
	
	// Check if expired
	if time.Now().After(entry.ExpiresAt) {
		c.logger.Warn("API CACHE MISS - expired", "url", url, "expired_at", entry.ExpiresAt)
		return nil, false
	}
	
	// No log for API cache hit to reduce noise
	return entry.Data, true
}

// CachedHTTPDo performs an HTTP request with caching support
func (d *Detector) cachedHTTPDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	// Only cache GET requests
	if req.Method != "GET" {
		return d.retryableHTTPDo(ctx, req)
	}
	
	// If cache is not available, fall back to non-cached request
	if d.cache == nil {
		return d.retryableHTTPDo(ctx, req)
	}
	
	url := req.URL.String()
	
	// Check cache
	cachedData, etag, found := d.cache.Get(url)
	if found {
		// Create a response from cached data
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(cachedData)),
			Header:     make(http.Header),
			Request:    req,
		}
		resp.Header.Set("X-From-Cache", "true")
		if etag != "" {
			resp.Header.Set("ETag", etag)
		}
		return resp, nil
	}
	
	// Make the actual request
	resp, err := d.retryableHTTPDo(ctx, req)
	if err != nil {
		return nil, err
	}
	
	// Only cache successful responses
	if resp.StatusCode == http.StatusOK {
		// Read the response body
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		
		// Cache the response
		etag := resp.Header.Get("ETag")
		if err := d.cache.Set(url, body, etag); err != nil {
			d.logger.Debug("cache set failed", "url", url, "error", err)
		}
		
		// Replace the response body with a new reader
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	
	return resp, nil
}

// Clean removes expired cache entries
func (c *DiskCache) Clean() error {
	return filepath.Walk(c.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		if info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip files we can't read
		}
		
		var entry CacheEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			// Remove invalid cache files
			os.Remove(path)
			return nil
		}
		
		if time.Now().After(entry.ExpiresAt) {
			c.logger.Debug("cleaning expired cache", "path", path)
			os.Remove(path)
		}
		
		return nil
	})
}