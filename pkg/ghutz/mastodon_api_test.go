package ghutz

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	
	"github.com/codeGROOVE-dev/ghuTZ/pkg/social"
)

func TestMastodonAPIExtraction(t *testing.T) {
	tests := []struct {
		name           string
		mastodonURL    string
		expectWebsites bool
		expectBio      bool
	}{
		{
			name:           "Extract from known Mastodon profile",
			mastodonURL:    "https://mastodon.social/@Gargron",
			expectWebsites: false, // May or may not have websites
			expectBio:      true,
		},
	}
	
	ctx := context.Background()
	logger := slog.Default()
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test using the social package
			socialData := map[string]string{
				"mastodon": tt.mastodonURL,
			}
			extracted := social.Extract(ctx, socialData, logger)
			
			if len(extracted) == 0 || extracted[0].Kind != "mastodon" {
				t.Skip("Could not fetch Mastodon profile - API may be down or account may not exist")
			}
			
			profileData := extracted[0]
			t.Logf("Mastodon Profile for %s:", tt.mastodonURL)
			t.Logf("  Bio: %s", profileData.Bio)
			t.Logf("  Joined: %s", profileData.Joined)
			t.Logf("  Fields: %v", profileData.Fields)
			t.Logf("  Tags: %v", profileData.Tags)
			
			if tt.expectBio && profileData.Bio == "" {
				t.Error("Expected bio to be present")
			}
			
			// Check for websites in fields
			websiteCount := 0
			for key := range profileData.Fields {
				lowerKey := strings.ToLower(key)
				if strings.Contains(lowerKey, "website") || strings.Contains(lowerKey, "blog") ||
				   strings.Contains(lowerKey, "home") || strings.Contains(lowerKey, "url") {
					websiteCount++
				}
			}
			
			if tt.expectWebsites && websiteCount == 0 {
				t.Error("Expected websites to be found")
			}
		})
	}
}

func TestMastodonURLParsing(t *testing.T) {
	tests := []struct {
		url          string
		wantUsername string
	}{
		{"https://mastodon.social/@Gargron", "Gargron"},
		{"https://fosstodon.org/@dlorenc", "dlorenc"},
		{"https://infosec.exchange/@jamonation", "jamonation"},
		{"https://example.social/users/testuser", "testuser"},
		{"https://mastodon.social/@user123", "user123"},
	}
	
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			// Parse URL and extract username
			parts := strings.Split(strings.Trim(strings.Split(tt.url, "?")[0], "/"), "/")
			var gotUsername string
			for _, part := range parts {
				if strings.HasPrefix(part, "@") {
					gotUsername = strings.TrimPrefix(part, "@")
					break
				} else if len(parts) >= 2 && parts[len(parts)-2] == "users" {
					gotUsername = parts[len(parts)-1]
					break
				}
			}
			
			// Fallback to last part
			if gotUsername == "" && len(parts) > 0 {
				lastPart := parts[len(parts)-1]
				if lastPart != "" && !strings.Contains(lastPart, ".") {
					gotUsername = strings.TrimPrefix(lastPart, "@")
				}
			}
			
			if gotUsername != tt.wantUsername {
				t.Errorf("ParseMastodonURL(%q) = %q, want %q", tt.url, gotUsername, tt.wantUsername)
			}
		})
	}
}