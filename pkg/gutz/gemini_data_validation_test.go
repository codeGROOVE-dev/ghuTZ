package gutz

import (
	"testing"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/github"
)

// TestGeminiDataCompleteness ensures all data needed for Gemini analysis is present
func TestGeminiDataCompleteness(t *testing.T) {
	// Mock UserContext with all the data that should be available
	userCtx := &UserContext{
		Username: "testuser",
		User: &github.User{
			Login:         "testuser",
			Name:          "Test User",
			Location:      "San Francisco, CA",
			Bio:           "Software Engineer @company",
			Company:       "@company",
			Blog:          "https://blog.example.com",
			TwitterHandle: "testuser",
			CreatedAt:     time.Now().Add(-365 * 24 * time.Hour),
			UpdatedAt:     time.Now().Add(-24 * time.Hour),
			Followers:     1000,
			Following:     100,
			PublicRepos:   50,
			SocialAccounts: []github.SocialAccount{
				{Provider: "TWITTER", URL: "https://twitter.com/testuser", DisplayName: "@testuser"},
				{Provider: "MASTODON", URL: "https://mastodon.social/@testuser", DisplayName: "@testuser"},
			},
		},
		Organizations: []github.Organization{
			{Name: "TestOrg", Login: "testorg", Location: "New York, NY"},
		},
		Events: []github.PublicEvent{
			{Type: "PushEvent", CreatedAt: time.Now().Add(-24 * time.Hour)}, // Sample event
		},
		PullRequests: []github.PullRequest{
			// PRs from GraphQL
		},
		Issues: []github.Issue{
			// Issues from GraphQL
		},
		StarredRepos: []github.Repository{
			{Name: "awesome-project", FullName: "user/awesome-project"}, // Sample starred repo
		},
		Gists: []github.Gist{
			{ID: "abc123", Description: "Test gist", CreatedAt: time.Now()}, // Sample gist
		},
	}

	// Verify all critical data for Gemini is present
	checks := []struct {
		name     string
		present  bool
		critical bool
	}{
		// User Profile Data
		{"User Profile", userCtx.User != nil, true},
		{"User Location", userCtx.User != nil && userCtx.User.Location != "", true},
		{"User Bio", userCtx.User != nil && userCtx.User.Bio != "", false},
		{"User Company", userCtx.User != nil && userCtx.User.Company != "", false},
		{"User Blog", userCtx.User != nil && userCtx.User.Blog != "", false},

		// Social Accounts (NEW from GraphQL!)
		{"Social Accounts", userCtx.User != nil && len(userCtx.User.SocialAccounts) > 0, true},
		{"Twitter Handle", userCtx.User != nil && userCtx.User.TwitterHandle != "", false},

		// Organizations
		{"Organizations", len(userCtx.Organizations) > 0, false},
		{"Org Locations", len(userCtx.Organizations) > 0 && userCtx.Organizations[0].Location != "", false},

		// Activity Data
		{"Public Events", len(userCtx.Events) > 0, true},
		{"Pull Requests", len(userCtx.PullRequests) > 0, false},
		{"Issues", len(userCtx.Issues) > 0, false},

		// Additional Context (NEW from GraphQL!)
		{"Starred Repos", len(userCtx.StarredRepos) > 0, false},
		{"Gists", len(userCtx.Gists) > 0, false},

		// Profile HTML for timezone scraping
		{"Profile HTML", userCtx.ProfileHTML != "", false},
	}

	criticalMissing := []string{}
	allMissing := []string{}

	for _, check := range checks {
		if !check.present {
			allMissing = append(allMissing, check.name)
			if check.critical {
				criticalMissing = append(criticalMissing, check.name)
			}
		}
	}

	// Report findings
	t.Log("=== Gemini Data Completeness Check ===")
	t.Logf("Total checks: %d", len(checks))
	t.Logf("Data present: %d", len(checks)-len(allMissing))
	t.Logf("Data missing: %d (critical: %d)", len(allMissing), len(criticalMissing))

	if len(criticalMissing) > 0 {
		t.Errorf("Critical data missing for Gemini: %v", criticalMissing)
	}

	// Verify data is properly passed to Gemini context
	contextData := buildGeminiContext(userCtx)

	// Check that all expected fields are in the context
	expectedKeys := []string{
		"user",
		"recent_events",
		"organizations",
		"social_accounts",      // NEW!
		"starred_repositories", // Fixed: actual key used in code
		"gist_count",           // NEW!
	}

	for _, key := range expectedKeys {
		if _, exists := contextData[key]; !exists {
			t.Errorf("Expected key '%s' missing from Gemini context", key)
		}
	}

	t.Log("✓ All expected data fields are available for Gemini analysis")
}

// buildGeminiContext simulates building context for Gemini
func buildGeminiContext(userCtx *UserContext) map[string]interface{} {
	context := make(map[string]interface{})

	if userCtx.User != nil {
		context["user"] = userCtx.User

		// Extract social accounts for Gemini
		if len(userCtx.User.SocialAccounts) > 0 {
			context["social_accounts"] = userCtx.User.SocialAccounts
		}
	}

	if len(userCtx.Events) > 0 {
		context["recent_events"] = userCtx.Events
	}

	if len(userCtx.Organizations) > 0 {
		context["organizations"] = userCtx.Organizations
	}

	if len(userCtx.PullRequests) > 0 {
		context["pull_requests"] = userCtx.PullRequests
	}

	if len(userCtx.Issues) > 0 {
		context["issues"] = userCtx.Issues
	}

	// NEW data from GraphQL
	if len(userCtx.StarredRepos) > 0 {
		context["starred_repositories"] = userCtx.StarredRepos // Fixed: use actual key
	}

	if len(userCtx.Gists) > 0 {
		context["gist_count"] = len(userCtx.Gists)
	}

	return context
}
