package ghutz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maypok86/otter/v2"
)

type CacheEntry struct {
	Data      []byte    `json:"data"`
	ExpiresAt time.Time `json:"expires_at"`
	ETag      string    `json:"etag,omitempty"`
}

type OtterCache struct {
	cache      otter.Cache[string, CacheEntry]
	dir        string
	ttl        time.Duration
	logger     *slog.Logger
	mu         sync.RWMutex
	saveCancel context.CancelFunc
	saveWg     sync.WaitGroup
}

func NewOtterCache(dir string, ttl time.Duration, logger *slog.Logger) (*OtterCache, error) {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}

	// Create otter cache with 100k capacity using v2 API
	cache := otter.Must(&otter.Options[string, CacheEntry]{
		MaximumSize:     100_000,
		InitialCapacity: 10_000,
		ExpiryCalculator: otter.ExpiryWriting[string, CacheEntry](ttl),
	})

	c := &OtterCache{
		cache:  *cache, // Dereference the pointer
		dir:    dir,
		ttl:    ttl,
		logger: logger,
	}

	// Load existing cache from disk
	if err := c.loadFromDisk(); err != nil {
		logger.Warn("failed to load cache from disk", "error", err)
	}
	// Log final cache state after loading
	logger.Info("cache initialized", "dir", dir, "entries_loaded", c.cache.EstimatedSize())

	// Start periodic save goroutine
	c.startPeriodicSave()

	return c, nil
}

func (c *OtterCache) getCacheKey(url string) string {
	h := sha256.New()
	h.Write([]byte(url))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *OtterCache) getCacheKeyForAPICall(url string, requestBody []byte) string {
	h := sha256.New()
	h.Write([]byte(url))
	h.Write(requestBody)
	return hex.EncodeToString(h.Sum(nil))
}

func (c *OtterCache) Get(url string) ([]byte, string, bool) {
	key := c.getCacheKey(url)
	
	entry, found := c.cache.GetIfPresent(key)
	if !found {
		c.logger.Debug("CACHE MISS - not found", "url", url)
		return nil, "", false
	}

	// Check if expired (otter should handle this, but double-check for safety)
	if time.Now().After(entry.ExpiresAt) {
		c.logger.Debug("CACHE MISS - expired", "url", url, "expired_at", entry.ExpiresAt)
		c.cache.Invalidate(key)
		return nil, "", false
	}

	return entry.Data, entry.ETag, true
}

func (c *OtterCache) Set(url string, data []byte, etag string) error {
	key := c.getCacheKey(url)
	
	entry := CacheEntry{
		Data:      data,
		ExpiresAt: time.Now().Add(c.ttl),
		ETag:      etag,
	}

	c.cache.Set(key, entry)
	c.logger.Debug("cache set", "url", url, "expires_at", entry.ExpiresAt, "size", len(data))
	return nil
}

func (c *OtterCache) SetAPICall(url string, requestBody []byte, data []byte) error {
	key := c.getCacheKeyForAPICall(url, requestBody)
	
	entry := CacheEntry{
		Data:      data,
		ExpiresAt: time.Now().Add(c.ttl),
		ETag:      "", // API calls don't typically use ETags
	}

	c.cache.Set(key, entry)
	c.logger.Debug("API cache set", "url", url, "expires_at", entry.ExpiresAt, "size", len(data))
	return nil
}

func (c *OtterCache) GetAPICall(url string, requestBody []byte) ([]byte, bool) {
	key := c.getCacheKeyForAPICall(url, requestBody)
	
	entry, found := c.cache.GetIfPresent(key)
	if !found {
		c.logger.Debug("API CACHE MISS - not found", "url", url)
		return nil, false
	}

	// Check if expired (otter should handle this, but double-check for safety)
	if time.Now().After(entry.ExpiresAt) {
		c.logger.Debug("API CACHE MISS - expired", "url", url, "expired_at", entry.ExpiresAt)
		c.cache.Invalidate(key)
		return nil, false
	}

	return entry.Data, true
}

func (c *OtterCache) loadFromDisk() error {
	cachePath := filepath.Join(c.dir, "otter-cache.gob")
	
	file, err := os.Open(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			c.logger.Info("no existing cache file found", "path", cachePath)
			return nil // No existing cache file
		}
		return fmt.Errorf("opening cache file: %w", err)
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	
	var entries map[string]CacheEntry
	if err := decoder.Decode(&entries); err != nil {
		return fmt.Errorf("decoding cache file: %w", err)
	}

	// Load entries into cache, filtering out expired ones
	now := time.Now()
	validEntries := 0
	for key, entry := range entries {
		if now.Before(entry.ExpiresAt) {
			c.cache.Set(key, entry)
			validEntries++
		}
	}

	c.logger.Info("successfully loaded cache from disk", 
		"path", cachePath,
		"total_entries", len(entries), 
		"valid_entries", validEntries,
		"expired_entries", len(entries)-validEntries)

	return nil
}

func (c *OtterCache) saveToDisk() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cachePath := filepath.Join(c.dir, "otter-cache.gob")
	
	// Create temporary file
	tempPath := cachePath + ".tmp"
	file, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("creating temp cache file: %w", err)
	}
	defer func() {
		file.Close()
		os.Remove(tempPath) // Clean up temp file if we fail
	}()

	// Collect all cache entries
	entries := make(map[string]CacheEntry)
	now := time.Now()
	
	// Use iterator to iterate over all entries in otter v2
	c.cache.All()(func(key string, entry CacheEntry) bool {
		// Only save non-expired entries
		if now.Before(entry.ExpiresAt) {
			entries[key] = entry
		}
		return true // Continue iteration
	})

	// Encode to gob format
	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(entries); err != nil {
		return fmt.Errorf("encoding cache to file: %w", err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("syncing cache file: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("closing cache file: %w", err)
	}

	// Atomically replace the old cache file
	if err := os.Rename(tempPath, cachePath); err != nil {
		return fmt.Errorf("replacing cache file: %w", err)
	}

	c.logger.Info("cache saved to disk", "entries", len(entries), "path", cachePath)
	return nil
}

func (c *OtterCache) startPeriodicSave() {
	ctx, cancel := context.WithCancel(context.Background())
	c.saveCancel = cancel

	c.saveWg.Add(1)
	go func() {
		defer c.saveWg.Done()
		
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.saveToDisk(); err != nil {
					c.logger.Error("periodic cache save failed", "error", err)
				}
			}
		}
	}()
}

func (c *OtterCache) Close() error {
	// Stop periodic saving
	if c.saveCancel != nil {
		c.saveCancel()
	}
	c.saveWg.Wait()

	// Final save before closing
	if err := c.saveToDisk(); err != nil {
		c.logger.Error("final cache save failed", "error", err)
		return err
	}

	// Otter v2 doesn't require explicit closing
	
	c.logger.Info("cache closed and saved to disk")
	return nil
}

func (c *OtterCache) Stats() map[string]interface{} {
	// Return basic stats since otter v2 doesn't expose detailed stats in the same way
	return map[string]interface{}{
		"size": c.cache.EstimatedSize(),
	}
}

// Clean is kept for backward compatibility but is now a no-op since otter handles TTL automatically
func (c *OtterCache) Clean() error {
	// Otter automatically removes expired entries, so this is a no-op
	stats := c.Stats()
	c.logger.Debug("cache stats", "stats", stats)
	return nil
}

// CachedHTTPDo performs an HTTP request with caching support.
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
		if closeErr := resp.Body.Close(); closeErr != nil {
			d.logger.Debug("failed to close response body", "error", closeErr)
		}
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