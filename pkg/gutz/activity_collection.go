package gutz

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/github"
)

// timestampEntry represents a single activity timestamp with metadata.
type timestampEntry struct {
	time       time.Time
	source     string // "event", "pr", "issue", "comment", "star", "commit"
	org        string // organization/owner name
	title      string // PR/issue title, or comment preview
	repository string // full repository name (owner/repo)
	url        string // URL to the item (for reference)
}

// collectActivityTimestampsWithContext gathers all activity timestamps from UserContext.
func (d *Detector) collectActivityTimestampsWithContext(
	ctx context.Context, userCtx *UserContext,
) (timestamps []timestampEntry, orgCounts map[string]int) {
	d.logger.Info("📊 Building unified timeline from UserContext",
		"username", userCtx.Username,
		"ssh_keys", len(userCtx.SSHKeys),
		"repos", len(userCtx.Repositories))

	allTimestamps, orgCounts := d.collectActivityTimestampsWithSSHKeys(ctx, userCtx.Username, userCtx.Events, userCtx.SSHKeys)

	// Add gist creation events to timeline
	gistCount := 0
	d.logger.Info("📝 Processing gists for timeline", "username", userCtx.Username, "total_gists", len(userCtx.Gists))
	for _, gist := range userCtx.Gists {
		if gist.CreatedAt.IsZero() || gist.CreatedAt.Year() < 2000 {
			continue
		}

		title := "created gist"
		if gist.Description != "" && len(gist.Description) <= 100 {
			title = "created gist: " + gist.Description
		}

		allTimestamps = append(allTimestamps, timestampEntry{
			time:       gist.CreatedAt,
			source:     "gist",
			org:        userCtx.Username, // Gists belong to the user
			title:      title,
			repository: "",
			url:        gist.HTMLURL,
		})
		gistCount++
		orgCounts[userCtx.Username]++
	}

	if gistCount > 0 {
		d.logger.Info("📝 Added gist creations to timeline", "username", userCtx.Username, "count", gistCount)
	}

	// Add repository creation events to timeline
	repoCount := 0
	d.logger.Debug("Processing repositories for timeline", "username", userCtx.Username, "total_repos", len(userCtx.Repositories))
	for i := range userCtx.Repositories {
		repo := &userCtx.Repositories[i]
		if i < 3 {
			d.logger.Debug("sample repo", "index", i, "name", repo.Name, "created_at", repo.CreatedAt, "fork", repo.Fork)
		}
		if repo.CreatedAt.IsZero() {
			d.logger.Debug("skipping repo with invalid date", "repo", repo.Name, "created_at", repo.CreatedAt)
			continue
		}

		// Skip forks - they weren't really "created" by the user
		if repo.Fork {
			d.logger.Debug("skipping forked repo", "repo", repo.Name)
			continue
		}

		// Use description as the main title if available, otherwise use a generic message
		title := repo.Description
		if title == "" {
			title = "created repository: " + repo.Name
		}

		d.logger.Debug("Adding repo to timeline", "repo", repo.Name, "created_at", repo.CreatedAt)
		allTimestamps = append(allTimestamps, timestampEntry{
			time:       repo.CreatedAt,
			source:     "repo_created",
			org:        userCtx.Username, // User's own repositories
			title:      title,
			repository: repo.FullName,
			url:        repo.HTMLURL,
		})
		repoCount++
		orgCounts[userCtx.Username]++
	}

	if repoCount > 0 {
		d.logger.Info("✅ Added repository creations to timeline", "username", userCtx.Username, "count", repoCount)
	} else {
		d.logger.Info("⚠️ No repository creations to add", "username", userCtx.Username, "total_repos", len(userCtx.Repositories))
	}

	// Add PRs from GraphQL (these supplement the event data which only covers ~30 days)
	prCount := 0
	d.logger.Debug("Processing PRs for timeline", "username", userCtx.Username, "total_prs", len(userCtx.PullRequests))
	for i := range userCtx.PullRequests {
		pr := &userCtx.PullRequests[i]
		if pr.CreatedAt.IsZero() || pr.CreatedAt.Year() < 2000 {
			d.logger.Debug("skipping PR with invalid date", "title", pr.Title, "created_at", pr.CreatedAt)
			continue
		}

		org := extractOrganization(pr.RepoName)
		allTimestamps = append(allTimestamps, timestampEntry{
			time:       pr.CreatedAt,
			source:     "pr",
			org:        org,
			title:      pr.Title,
			repository: pr.RepoName,
			url:        pr.HTMLURL,
		})
		prCount++
		if org != "" {
			orgCounts[org]++
		}
	}

	if prCount > 0 {
		d.logger.Info("🔀 Added pull requests to timeline", "username", userCtx.Username, "count", prCount)
	}

	// Add Issues from GraphQL (these supplement the event data which only covers ~30 days)
	issueCount := 0
	d.logger.Debug("Processing issues for timeline", "username", userCtx.Username, "total_issues", len(userCtx.Issues))
	for i := range userCtx.Issues {
		issue := &userCtx.Issues[i]
		if issue.CreatedAt.IsZero() || issue.CreatedAt.Year() < 2000 {
			d.logger.Debug("skipping issue with invalid date", "title", issue.Title, "created_at", issue.CreatedAt)
			continue
		}

		org := extractOrganization(issue.RepoName)
		allTimestamps = append(allTimestamps, timestampEntry{
			time:       issue.CreatedAt,
			source:     "issue",
			org:        org,
			title:      issue.Title,
			repository: issue.RepoName,
			url:        issue.HTMLURL,
		})
		issueCount++
		if org != "" {
			orgCounts[org]++
		}
	}

	if issueCount > 0 {
		d.logger.Info("🐛 Added issues to timeline", "username", userCtx.Username, "count", issueCount)
	}

	// Don't call collectSupplementalTimestamps here - we already have all the data
	// from UserContext. The supplemental data fetching happens separately in
	// analyzeActivityTimestampsWithoutSupplemental

	d.logger.Info("📊 Unified timeline built",
		"username", userCtx.Username,
		"total_events", len(allTimestamps))

	return allTimestamps, orgCounts
}

// collectActivityTimestampsWithSSHKeys gathers all activity timestamps including SSH keys.
//
//nolint:gocognit,revive,maintidx // Complex event processing logic
func (d *Detector) collectActivityTimestampsWithSSHKeys(_ context.Context, username string,
	events []github.PublicEvent, sshKeys []github.SSHKey,
) (timestamps []timestampEntry, orgCounts map[string]int) {
	allTimestamps := []timestampEntry{}
	orgCounts = make(map[string]int)

	// Add events
	eventOldest := time.Now()
	eventNewest := time.Time{}
	zeroTimeCount := 0
	for _, event := range events {
		// SECURITY: Filter out zero timestamps (0001-01-01)
		if event.CreatedAt.IsZero() || event.CreatedAt.Year() < 2000 {
			zeroTimeCount++
			d.logger.Warn("skipping event with invalid timestamp",
				"username", username,
				"timestamp", event.CreatedAt,
				"repo", event.Repo.Name,
				"type", event.Type)
			continue
		}

		org := extractOrganization(event.Repo.Name)

		// Extract comment body from events if available
		eventTitle := event.Type
		eventSource := "event"

		// For comment events, try to extract the comment body
		switch event.Type {
		case "IssueCommentEvent", "PullRequestReviewCommentEvent", "PullRequestReviewEvent":
			var payload map[string]any
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				// Try to extract comment body
				var commentBody string
				if comment, ok := payload["comment"].(map[string]any); ok {
					if body, ok := comment["body"].(string); ok {
						commentBody = body
					}
				} else if review, ok := payload["review"].(map[string]any); ok {
					// For PR reviews
					if body, ok := review["body"].(string); ok {
						commentBody = body
					}
				}

				if commentBody != "" {
					// Truncate for title field
					if len(commentBody) > 150 {
						eventTitle = commentBody[:150] + "..."
					} else {
						eventTitle = commentBody
					}
					eventSource = "comment" // Mark as comment for text sample collection
				}
			}
		case "PushEvent":
			// For PushEvents, extract the most recent commit message
			var payload map[string]any
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				if commits, ok := payload["commits"].([]any); ok && len(commits) > 0 {
					// Get the most recent commit (last in the array)
					if lastCommit, ok := commits[len(commits)-1].(map[string]any); ok {
						if message, ok := lastCommit["message"].(string); ok && message != "" {
							// Truncate commit message if too long
							if len(message) > 150 { //nolint:revive // Simple truncation logic
								eventTitle = message[:150] + "..."
							} else {
								eventTitle = message
							}
							eventSource = "commit" // Mark as commit for text sample collection
						}
					}
				}
			}
		case "PullRequestEvent":
			// For PullRequestEvents, extract the PR title
			var payload map[string]any
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				if pr, ok := payload["pull_request"].(map[string]any); ok {
					if title, ok := pr["title"].(string); ok && title != "" {
						// Truncate PR title if too long
						if len(title) > 150 {
							eventTitle = title[:150] + "..."
						} else {
							eventTitle = title
						}
						eventSource = "pr" // Mark as PR for text sample collection
					}
				}
			}
		case "IssuesEvent":
			// For IssuesEvents, extract the issue title
			var payload map[string]any
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				if issue, ok := payload["issue"].(map[string]any); ok {
					if title, ok := issue["title"].(string); ok && title != "" {
						// Truncate issue title if too long
						if len(title) > 150 {
							eventTitle = title[:150] + "..."
						} else {
							eventTitle = title
						}
						eventSource = "issue" // Mark as issue for text sample collection
					} else {
						d.logger.Debug("IssuesEvent missing title", "username", username,
							"has_issue", ok, "title_type", fmt.Sprintf("%T", issue["title"]))
					}
				} else {
					d.logger.Debug("IssuesEvent missing issue object", "username", username,
						"payload_keys", fmt.Sprintf("%v", reflect.ValueOf(payload).MapKeys()))
				}
			} else {
				d.logger.Debug("IssuesEvent failed to unmarshal", "username", username, "error", err)
			}
		default:
			// Keep the default event type for other events
		}

		allTimestamps = append(allTimestamps, timestampEntry{
			time:       event.CreatedAt,
			source:     eventSource,
			org:        org,
			title:      eventTitle,
			repository: event.Repo.Name,
			url:        event.Repo.URL,
		})
		if event.CreatedAt.Before(eventOldest) {
			eventOldest = event.CreatedAt
		}
		if event.CreatedAt.After(eventNewest) {
			eventNewest = event.CreatedAt
		}
	}

	if zeroTimeCount > 0 {
		d.logger.Warn("filtered out events with zero/invalid timestamps",
			"username", username,
			"count", zeroTimeCount,
			"total_events", len(events))
	}

	if len(events) > 0 {
		d.logger.Debug("GitHub Events data", "username", username,
			"count", len(events),
			"oldest", eventOldest.Format("2006-01-02"),
			"newest", eventNewest.Format("2006-01-02"),
			"days_covered", int(eventNewest.Sub(eventOldest).Hours()/24))
	}

	// Note: Gists are added separately in collectActivityTimestampsWithContext
	// from UserContext.Gists which contains full gist objects with descriptions

	// Add SSH keys to timeline if provided
	if sshKeys != nil {
		sshKeyCount := 0
		d.logger.Debug("processing SSH keys for timeline", "username", username, "total_keys", len(sshKeys))
		for _, sshKey := range sshKeys {
			if sshKey.CreatedAt.IsZero() || sshKey.CreatedAt.Year() < 2000 {
				d.logger.Debug("skipping SSH key with invalid date", "username", username, "created_at", sshKey.CreatedAt)
				continue
			}

			title := "added SSH key"
			if sshKey.Title != "" {
				title = "added SSH key: " + sshKey.Title
			}

			allTimestamps = append(allTimestamps, timestampEntry{
				time:       sshKey.CreatedAt,
				source:     "ssh_key",
				org:        username, // SSH keys belong to the user
				title:      title,
				repository: "",
				url:        sshKey.URL,
			})
			sshKeyCount++
		}
		if sshKeyCount > 0 {
			d.logger.Debug("added SSH keys to timeline", "username", username, "count", sshKeyCount)
		} else {
			d.logger.Debug("no valid SSH keys to add to timeline", "username", username)
		}
	} else {
		d.logger.Debug("no SSH keys provided for timeline", "username", username)
	}

	// Log timestamps without org associations for debugging
	noOrgCount := 0
	orgCount := 0
	for _, ts := range allTimestamps {
		if ts.org == "" {
			noOrgCount++
			d.logger.Debug("timestamp without org", "source", ts.source, "time", ts.time.Format("2006-01-02 15:04"))
		} else {
			orgCount++
			orgCounts[ts.org]++
		}
	}
	d.logger.Info("org association summary", "username", username, "with_org", orgCount, "without_org", noOrgCount, "total", len(allTimestamps))

	return allTimestamps, orgCounts
}

// extractOrganization extracts the organization from a repository path.
func extractOrganization(repository string) string {
	if repository == "" {
		return ""
	}
	if idx := strings.Index(repository, "/"); idx > 0 {
		return repository[:idx]
	}
	return ""
}

// filterAndSortTimestamps filters timestamps by age and sorts them.
func filterAndSortTimestamps(allTimestamps []timestampEntry, maxYears int) []timestampEntry {
	// Sort timestamps by recency (newest first)
	sort.Slice(allTimestamps, func(i, j int) bool {
		return allTimestamps[i].time.After(allTimestamps[j].time)
	})

	// Filter out events older than maxYears to avoid stale patterns
	cutoffTime := time.Now().AddDate(-maxYears, 0, 0)
	filtered := []timestampEntry{}
	for _, ts := range allTimestamps {
		if ts.time.After(cutoffTime) {
			filtered = append(filtered, ts)
		}
	}

	return filtered
}

// applyProgressiveTimeWindow applies a progressive time window strategy to get sufficient data.
func applyProgressiveTimeWindow(allTimestamps []timestampEntry, targetMin int) []timestampEntry {
	const maxTimeWindowDays = 365 * 5 // Maximum 5 years
	const initialWindowDays = 30      // Start with 30 days for recency preference
	const minTimeSpanDays = 30        // Minimum time span we want to achieve
	const expansionFactor = 1.25      // Increase by 25% each iteration (30→37.5→46.9→58.6→73.3→91.6...)

	// Progressive time window strategy
	timeWindowDays := float64(initialWindowDays)
	var filtered []timestampEntry

	for timeWindowDays <= maxTimeWindowDays {
		cutoffTime := time.Now().AddDate(0, 0, -int(timeWindowDays))

		// Use map to deduplicate timestamps during filtering
		uniqueTimestamps := make(map[time.Time]timestampEntry)
		for _, ts := range allTimestamps {
			if ts.time.After(cutoffTime) {
				// Keep the first occurrence of each timestamp
				if _, exists := uniqueTimestamps[ts.time]; !exists {
					uniqueTimestamps[ts.time] = ts
				}
			}
		}

		// Convert back to slice
		filtered = []timestampEntry{}
		for _, ts := range uniqueTimestamps {
			filtered = append(filtered, ts)
		}

		// Calculate actual time span of the filtered data
		var actualSpanDays int
		if len(filtered) > 0 {
			var oldest, newest time.Time
			for i, ts := range filtered {
				if i == 0 || ts.time.Before(oldest) {
					oldest = ts.time
				}
				if i == 0 || ts.time.After(newest) {
					newest = ts.time
				}
			}
			actualSpanDays = int(newest.Sub(oldest).Hours() / 24)
		}

		// Count data sources in filtered set for debugging
		sourceCounts := make(map[string]int)
		for _, ts := range filtered {
			sourceCounts[ts.source]++
		}

		// Stop if we have enough events AND sufficient time span, or hit max window
		hasEnoughEvents := len(filtered) >= targetMin
		hasEnoughTimeSpan := actualSpanDays >= minTimeSpanDays

		if (hasEnoughEvents && hasEnoughTimeSpan) || timeWindowDays >= maxTimeWindowDays {
			break
		}

		// Expand the window
		timeWindowDays *= expansionFactor
	}

	return filtered
}
