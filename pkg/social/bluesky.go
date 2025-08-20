package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/retry"
)

// BlueSkyProfile represents a BlueSky profile from the API.
type BlueSkyProfile struct {
	Did            string   `json:"did"`
	Handle         string   `json:"handle"`
	DisplayName    string   `json:"displayName"`
	Description    string   `json:"description"`
	Avatar         string   `json:"avatar"`
	Banner         string   `json:"banner"`
	CreatedAt      string   `json:"createdAt"`
	Labels         []string `json:"labels"`
	FollowersCount int      `json:"followersCount"`
	FollowingCount int      `json:"followingCount"`
	PostsCount     int      `json:"postsCount"`
}

// extractBlueSky extracts content from a BlueSky profile.
func extractBlueSky(ctx context.Context, blueSkyURL string, logger *slog.Logger) *Content {
	// Extract handle from URL
	handle := extractBlueSkyHandle(blueSkyURL)
	if handle == "" {
		logger.Debug("could not extract BlueSky handle", "url", blueSkyURL)
		return nil
	}

	// Try the public API endpoint
	profile, err := fetchBlueSkyProfile(ctx, handle, logger)
	if err != nil {
		logger.Debug("failed to fetch BlueSky profile", "handle", handle, "error", err)
		// Return basic structure
		return &Content{
			Kind:     "bluesky",
			URL:      blueSkyURL,
			Username: handle,
			Fields:   make(map[string]string),
			Markdown: fmt.Sprintf("# BlueSky Profile\n\n**Handle:** @%s\n\n*Note: Could not fetch full profile data*\n", handle),
		}
	}

	// Create content structure with BlueSky data
	content := &Content{
		Kind:     "bluesky",
		URL:      blueSkyURL,
		Username: handle,
		Name:     profile.DisplayName,
		Bio:      profile.Description,
		Tags:     extractHashtags(profile.Description),
		Fields:   make(map[string]string),
	}

	// Add additional fields
	if profile.CreatedAt != "" {
		content.Fields["joined"] = profile.CreatedAt
	}
	if profile.FollowersCount > 0 {
		content.Fields["followers"] = strconv.Itoa(profile.FollowersCount)
	}
	if profile.FollowingCount > 0 {
		content.Fields["following"] = strconv.Itoa(profile.FollowingCount)
	}

	// Build markdown
	var md strings.Builder
	md.WriteString("# BlueSky Profile\n\n")
	md.WriteString("**Handle:** @" + handle + "\n")
	if profile.DisplayName != "" {
		md.WriteString("**Name:** " + profile.DisplayName + "\n")
	}
	if profile.Description != "" {
		md.WriteString("\n**Bio:**\n" + profile.Description + "\n")
	}
	if len(content.Tags) > 0 {
		md.WriteString("\n**Hashtags:** " + strings.Join(content.Tags, ", ") + "\n")
	}

	content.Markdown = md.String()

	logger.Debug("successfully extracted BlueSky profile",
		"url", blueSkyURL,
		"handle", handle,
		"name", profile.DisplayName,
		"bio_length", len(profile.Description))

	return content
}

// extractBlueSkyHandle extracts the handle from a BlueSky URL.
func extractBlueSkyHandle(blueSkyURL string) string {
	// Handle patterns:
	// https://bsky.app/profile/handle.bsky.social
	// https://bsky.app/profile/custom.domain
	// https://staging.bsky.app/profile/handle

	handle := ""
	if strings.Contains(blueSkyURL, "bsky.app/profile/") {
		parts := strings.Split(blueSkyURL, "bsky.app/profile/")
		if len(parts) > 1 {
			handle = strings.Split(parts[1], "/")[0]
			handle = strings.Split(handle, "?")[0]
			handle = strings.TrimSpace(handle)
		}
	}

	return handle
}

// fetchBlueSkyProfile fetches a BlueSky profile using the public API.
func fetchBlueSkyProfile(ctx context.Context, handle string, logger *slog.Logger) (*BlueSkyProfile, error) {
	// Try the public API endpoint
	// Note: BlueSky's API is evolving, this endpoint might change
	apiURL := fmt.Sprintf("https://public.api.bsky.app/xrpc/app.bsky.actor.getProfile?actor=%s", handle)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "guTZ/1.0")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Use retry logic with exponential backoff and jitter
	var resp *http.Response
	err = retry.Do(
		func() error {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close() //nolint:errcheck // best effort close on retry
			}
			var doErr error
			resp, doErr = client.Do(req) //nolint:bodyclose // response body closed in defer or on retry
			if doErr != nil {
				return doErr
			}
			// Retry on server errors and rate limiting
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
				body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
				if readErr != nil {
					_ = resp.Body.Close() //nolint:errcheck // best effort close on error path
					return fmt.Errorf("HTTP %d: failed to read body: %w", resp.StatusCode, readErr)
				}
				if closeErr := resp.Body.Close(); closeErr != nil {
					logger.Debug("failed to close response body", "error", closeErr)
				}
				return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			}
			return nil
		},
		retry.Context(ctx),
		retry.Attempts(5),
		retry.Delay(time.Second),
		retry.MaxDelay(2*time.Minute),
		retry.DelayType(retry.FullJitterBackoffDelay),
		retry.OnRetry(func(n uint, err error) {
			logger.Debug("retrying BlueSky API fetch", "attempt", n+1, "url", apiURL, "error", err)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching profile after retries: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if err != nil {
			logger.Debug("failed to read error response body", "error", err)
			return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
		}
		logger.Debug("BlueSky API returned non-200 status",
			"status", resp.StatusCode,
			"body", string(body))
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var profile BlueSkyProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &profile, nil
}
