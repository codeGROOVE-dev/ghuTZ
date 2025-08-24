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

// collectActivityTimestamps gathers all activity timestamps from various sources.
func (d *Detector) collectActivityTimestamps(ctx context.Context, username string,
	events []github.PublicEvent,
) (timestamps []timestampEntry, orgCounts map[string]int) {
	return d.collectActivityTimestampsWithSSHKeys(ctx, username, events, nil)
}

// collectActivityTimestampsWithContext gathers all activity timestamps from UserContext.
func (d *Detector) collectActivityTimestampsWithContext(ctx context.Context, userCtx *UserContext) (timestamps []timestampEntry, orgCounts map[string]int) {
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
	for i, repo := range userCtx.Repositories {
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
	for _, pr := range userCtx.PullRequests {
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
	for _, issue := range userCtx.Issues {
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
func (d *Detector) collectActivityTimestampsWithSSHKeys(ctx context.Context, username string,
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
			var payload map[string]interface{}
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				// Try to extract comment body
				var commentBody string
				if comment, ok := payload["comment"].(map[string]interface{}); ok {
					if body, ok := comment["body"].(string); ok {
						commentBody = body
					}
				} else if review, ok := payload["review"].(map[string]interface{}); ok {
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
			var payload map[string]interface{}
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				if commits, ok := payload["commits"].([]interface{}); ok && len(commits) > 0 {
					// Get the most recent commit (last in the array)
					if lastCommit, ok := commits[len(commits)-1].(map[string]interface{}); ok {
						if message, ok := lastCommit["message"].(string); ok && message != "" {
							// Truncate commit message if too long
							if len(message) > 150 {
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
			var payload map[string]interface{}
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				if pr, ok := payload["pull_request"].(map[string]interface{}); ok {
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
			var payload map[string]interface{}
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				if issue, ok := payload["issue"].(map[string]interface{}); ok {
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

// collectSupplementalTimestamps fetches additional activity data when needed.
func (d *Detector) collectSupplementalTimestamps(ctx context.Context, username string,
	allTimestamps []timestampEntry, targetDataPoints int,
) []timestampEntry {
	// Deduplicate timestamps first to get accurate count
	uniqueTimestamps := make(map[time.Time]bool)
	var uniqueEntries []timestampEntry
	for _, entry := range allTimestamps {
		if !uniqueTimestamps[entry.time] {
			uniqueTimestamps[entry.time] = true
			uniqueEntries = append(uniqueEntries, entry)
		}
	}
	allTimestamps = uniqueEntries

	// First, count how many events we have in the last 3 months from first-page data
	threeMonthsAgo := time.Now().AddDate(0, -3, 0)
	recentEvents := 0
	for _, ts := range allTimestamps {
		if ts.time.After(threeMonthsAgo) {
			recentEvents++
		}
	}

	d.logger.Debug("recent activity check", "username", username,
		"events_last_3_months", recentEvents,
		"target", targetDataPoints,
		"need_second_page", recentEvents < targetDataPoints)

	d.logger.Info("📊 Fetching supplemental data", "username", username,
		"current_count", len(allTimestamps),
		"target_count", targetDataPoints,
		"recent_events_3mo", recentEvents)

	// Always fetch first page of supplemental data for proper time span analysis
	// Only fetch additional pages if we need more recent data
	maxPages := 1
	if recentEvents < targetDataPoints {
		maxPages = 2 // Fetch deeper if we need more recent activity
		d.logger.Debug("insufficient recent activity, fetching additional pages",
			"username", username, "recent_events", recentEvents, "max_pages", maxPages)
	} else {
		d.logger.Debug("sufficient recent activity, limiting to first page",
			"username", username, "recent_events", recentEvents, "max_pages", maxPages)
	}

	// Fetch supplemental data with appropriate depth
	additionalData := d.fetchSupplementalActivityWithDepth(ctx, username, maxPages)

	// Add all timestamps from supplemental data
	allTimestamps = d.addSupplementalData(allTimestamps, additionalData, username)

	// Check if we still need more data after initial fetch
	if len(allTimestamps) < targetDataPoints {
		allTimestamps = d.fetchAdditionalPages(ctx, username, allTimestamps, targetDataPoints, additionalData)
	}

	return allTimestamps
}

// fetchAdditionalPages fetches additional pages of data when needed.
func (d *Detector) fetchAdditionalPages(ctx context.Context, username string,
	allTimestamps []timestampEntry, targetDataPoints int, additionalData *ActivityData,
) []timestampEntry {
	remaining := targetDataPoints - len(allTimestamps)
	d.logger.Info("📊 Still need more data, fetching additional pages", "username", username,
		"current_count", len(allTimestamps),
		"need", remaining,
		"fetching", "PRs page 2+, Issues page 2+, Commits page 2")

	// Fetch second page of PRs, issues, and commits in parallel
	extraData := d.fetchSupplementalActivityWithDepth(ctx, username, 2)

	// Only add the NEW data from pages 2+ (first 100 already included)
	prCount := len(additionalData.PullRequests)
	issueCount := len(additionalData.Issues)

	// Add new PRs (beyond first 100)
	if len(extraData.PullRequests) > prCount {
		newPRs := extraData.PullRequests[prCount:]
		for i := range newPRs {
			org := extractOrganization(newPRs[i].RepoName)
			allTimestamps = append(allTimestamps, timestampEntry{
				time:       newPRs[i].CreatedAt,
				source:     "pr",
				org:        org,
				title:      newPRs[i].Title,
				repository: newPRs[i].RepoName,
				url:        newPRs[i].HTMLURL,
			})
		}
		d.logger.Debug("added additional PRs", "username", username, "count", len(newPRs))
	}

	// Add new issues (beyond first 100)
	if len(extraData.Issues) > issueCount {
		newIssues := extraData.Issues[issueCount:]
		for i := range newIssues {
			org := extractOrganization(newIssues[i].RepoName)
			allTimestamps = append(allTimestamps, timestampEntry{
				time:       newIssues[i].CreatedAt,
				source:     "issue",
				org:        org,
				title:      newIssues[i].Title,
				repository: newIssues[i].RepoName,
				url:        newIssues[i].HTMLURL,
			})
		}
		d.logger.Debug("added additional issues", "username", username, "count", len(newIssues))
	}

	d.logger.Debug("final timestamp count after extra fetch", "username", username,
		"total", len(allTimestamps))

	return allTimestamps
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

// addSupplementalData adds supplemental activity data to timestamps.
func (d *Detector) addSupplementalData(allTimestamps []timestampEntry, additionalData *ActivityData, username string) []timestampEntry {
	prOldest := time.Now()
	prNewest := time.Time{}
	prZeroCount := 0
	for i := range additionalData.PullRequests {
		pr := &additionalData.PullRequests[i]
		// Filter out zero timestamps
		if pr.CreatedAt.IsZero() || pr.CreatedAt.Year() < 2000 {
			prZeroCount++
			d.logger.Warn("skipping PR with invalid timestamp",
				"username", username,
				"timestamp", pr.CreatedAt,
				"repo", pr.RepoName,
				"title", pr.Title)
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
		if pr.CreatedAt.Before(prOldest) {
			prOldest = pr.CreatedAt
		}
		if pr.CreatedAt.After(prNewest) {
			prNewest = pr.CreatedAt
		}
	}

	if prZeroCount > 0 {
		d.logger.Warn("filtered out PRs with zero/invalid timestamps",
			"username", username,
			"count", prZeroCount,
			"total_prs", len(additionalData.PullRequests))
	}

	if len(additionalData.PullRequests) > 0 {
		d.logger.Debug("Pull Requests data", "username", username,
			"count", len(additionalData.PullRequests),
			"oldest", prOldest.Format("2006-01-02"),
			"newest", prNewest.Format("2006-01-02"),
			"days_covered", int(prNewest.Sub(prOldest).Hours()/24))
	}

	issueOldest := time.Now()
	issueNewest := time.Time{}
	issueZeroCount := 0
	for i := range additionalData.Issues {
		issue := &additionalData.Issues[i]
		// Filter out zero timestamps
		if issue.CreatedAt.IsZero() || issue.CreatedAt.Year() < 2000 {
			issueZeroCount++
			d.logger.Warn("skipping issue with invalid timestamp",
				"username", username,
				"timestamp", issue.CreatedAt,
				"repo", issue.RepoName,
				"title", issue.Title)
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
		if issue.CreatedAt.Before(issueOldest) {
			issueOldest = issue.CreatedAt
		}
		if issue.CreatedAt.After(issueNewest) {
			issueNewest = issue.CreatedAt
		}
	}

	if issueZeroCount > 0 {
		d.logger.Warn("filtered out issues with zero/invalid timestamps",
			"username", username,
			"count", issueZeroCount,
			"total_issues", len(additionalData.Issues))
	}

	if len(additionalData.Issues) > 0 {
		d.logger.Debug("Issues data", "username", username,
			"count", len(additionalData.Issues),
			"oldest", issueOldest.Format("2006-01-02"),
			"newest", issueNewest.Format("2006-01-02"),
			"days_covered", int(issueNewest.Sub(issueOldest).Hours()/24))
	}

	commentOldest := time.Now()
	commentNewest := time.Time{}
	commentZeroCount := 0
	for _, comment := range additionalData.Comments {
		// Filter out zero timestamps
		if comment.CreatedAt.IsZero() || comment.CreatedAt.Year() < 2000 {
			commentZeroCount++
			d.logger.Warn("skipping comment with invalid timestamp",
				"username", username,
				"timestamp", comment.CreatedAt)
			continue
		}
		org := extractOrganization(comment.Repository)
		// Truncate comment body for title field
		commentPreview := comment.Body
		if len(commentPreview) > 150 {
			commentPreview = commentPreview[:150] + "..."
		}
		allTimestamps = append(allTimestamps, timestampEntry{
			time:       comment.CreatedAt,
			source:     "comment",
			org:        org,
			title:      commentPreview,
			repository: comment.Repository,
			url:        comment.HTMLURL,
		})
		if comment.CreatedAt.Before(commentOldest) {
			commentOldest = comment.CreatedAt
		}
		if comment.CreatedAt.After(commentNewest) {
			commentNewest = comment.CreatedAt
		}
	}

	if len(additionalData.Comments) > 0 {
		d.logger.Debug("Comments data", "username", username,
			"count", len(additionalData.Comments),
			"oldest", commentOldest.Format("2006-01-02"),
			"newest", commentNewest.Format("2006-01-02"),
			"days_covered", int(commentNewest.Sub(commentOldest).Hours()/24))
	}

	// Add commit activities to timeline
	for i := range additionalData.CommitActivities {
		commit := &additionalData.CommitActivities[i]
		if commit.AuthorDate.IsZero() || commit.AuthorDate.Year() < 2000 {
			continue
		}
		org := extractOrganization(commit.Repository)
		allTimestamps = append(allTimestamps, timestampEntry{
			time:       commit.AuthorDate,
			source:     "commit",
			org:        org,
			title:      "commit", // We don't have the message in CommitActivity struct
			repository: commit.Repository,
			url:        "",
		})
	}

	// Add starred repositories to timeline
	for i, starTime := range additionalData.StarTimestamps {
		if starTime.IsZero() || starTime.Year() < 2000 {
			continue
		}

		// Get repository name if available
		var repoName string
		if i < len(additionalData.StarredRepos) {
			repoName = additionalData.StarredRepos[i].FullName
		}

		org := extractOrganization(repoName)
		allTimestamps = append(allTimestamps, timestampEntry{
			time:       starTime,
			source:     "star",
			org:        org,
			title:      "starred " + repoName,
			repository: repoName,
			url:        "",
		})
	}

	// Note: SSH keys are added separately in collectActivityTimestampsWithContext
	// since they come from UserContext, not from supplemental fetching

	d.logger.Debug("collected all timestamps", "username", username,
		"total_before_dedup", len(allTimestamps),
		"prs", len(additionalData.PullRequests),
		"issues", len(additionalData.Issues),
		"comments", len(additionalData.Comments),
		"commits", len(additionalData.CommitActivities),
		"stars", len(additionalData.StarTimestamps),
		"starred_repos", len(additionalData.StarredRepos))

	return allTimestamps
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
