package gutz

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/github"
)

// timestampEntry represents a single activity timestamp with metadata.
type timestampEntry struct {
	time   time.Time
	source string // for debugging
	org    string // organization/owner name
}

// collectActivityTimestamps gathers all activity timestamps from various sources.
func (d *Detector) collectActivityTimestamps(ctx context.Context, username string,
	events []github.PublicEvent,
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
		allTimestamps = append(allTimestamps, timestampEntry{
			time:   event.CreatedAt,
			source: "event",
			org:    org,
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

	// Also fetch gist timestamps
	if gistTimestamps, err := d.githubClient.FetchUserGists(ctx, username); err == nil && len(gistTimestamps) > 0 {
		gistZeroCount := 0
		for _, ts := range gistTimestamps {
			// Filter out zero timestamps
			if ts.IsZero() || ts.Year() < 2000 {
				gistZeroCount++
				d.logger.Warn("skipping gist with invalid timestamp",
					"username", username,
					"timestamp", ts)
				continue
			}
			allTimestamps = append(allTimestamps, timestampEntry{
				time:   ts,
				source: "gist",
				org:    username, // gists are owned by the user
			})
		}
		d.logger.Debug("added gist timestamps", "username", username, "count", len(gistTimestamps))
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
	const minDaysSpan = 30 // Need at least 4 weeks for good pattern detection

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
				time:   newPRs[i].CreatedAt,
				source: "pr",
				org:    org,
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
				time:   newIssues[i].CreatedAt,
				source: "issue",
				org:    org,
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
			time:   pr.CreatedAt,
			source: "pr",
			org:    org,
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
			time:   issue.CreatedAt,
			source: "issue",
			org:    org,
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
		allTimestamps = append(allTimestamps, timestampEntry{
			time:   comment.CreatedAt,
			source: "comment",
			org:    org,
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

	// Note: Starred repositories from the API don't have timestamps directly,
	// but they're fetched with timestamps in FetchStarredRepositories
	// This is handled separately in the main activity collection
	// Currently not processing organization timestamps for starred repos
	// since they come from a different API call with timestamp data

	d.logger.Debug("collected all timestamps", "username", username,
		"total_before_dedup", len(allTimestamps),
		"prs", len(additionalData.PullRequests),
		"issues", len(additionalData.Issues),
		"comments", len(additionalData.Comments),
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

		for source, count := range sourceCounts {
			fmt.Printf("  - %s: %d events\n", source, count)
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
