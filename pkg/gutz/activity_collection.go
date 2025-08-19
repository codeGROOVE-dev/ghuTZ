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
	const minDaysSpan = 14 // Need at least 2 weeks for good pattern detection

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

	// Calculate current time span
	var timeSpanDays int
	if len(allTimestamps) > 0 {
		var oldest, newest time.Time
		for i, ts := range allTimestamps {
			if i == 0 || ts.time.Before(oldest) {
				oldest = ts.time
			}
			if i == 0 || ts.time.After(newest) {
				newest = ts.time
			}
		}
		timeSpanDays = int(newest.Sub(oldest).Hours() / 24)
	}

	needSupplemental := len(allTimestamps) < targetDataPoints || timeSpanDays < minDaysSpan

	if !needSupplemental {
		return allTimestamps
	}

	constraints := []string{}
	if len(allTimestamps) < targetDataPoints {
		constraints = append(constraints, fmt.Sprintf("need %d more data points", targetDataPoints-len(allTimestamps)))
	}
	if timeSpanDays < minDaysSpan {
		constraints = append(constraints, fmt.Sprintf("need %d more days coverage", minDaysSpan-timeSpanDays))
	}

	d.logger.Info("📊 Fetching supplemental data", "username", username,
		"current_count", len(allTimestamps),
		"target_count", targetDataPoints,
		"current_days", timeSpanDays,
		"target_days", minDaysSpan,
		"constraints", strings.Join(constraints, ", "))

	// Fetch ALL additional data from all sources (first page only for performance)
	additionalData := d.fetchSupplementalActivityWithDepth(ctx, username, 1)

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

	// Add starred repository timestamps (if any were fetched)
	for i := range additionalData.StarredRepos {
		repo := &additionalData.StarredRepos[i]
		// Note: Starred repositories from the API don't have timestamps directly,
		// but they're fetched with timestamps in FetchStarredRepositories
		// This is handled separately in the main activity collection
		org := extractOrganization(repo.FullName)
		if org != "" {
			// We'll add these timestamps in a different way since starred repo 
			// timestamps come from a different API call with timestamp data
		}
	}

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
	const initialWindowDays = 30      // Start with 1 month
	const expansionFactor = 1.25      // Increase by 25% each iteration

	// Progressive time window strategy
	timeWindowDays := float64(initialWindowDays)
	var filtered []timestampEntry

	for timeWindowDays <= maxTimeWindowDays {
		cutoffTime := time.Now().AddDate(0, 0, -int(timeWindowDays))
		filtered = []timestampEntry{}

		for _, ts := range allTimestamps {
			if ts.time.After(cutoffTime) {
				filtered = append(filtered, ts)
			}
		}

		// If we have enough events or we've hit max window, stop
		if len(filtered) >= targetMin || timeWindowDays >= maxTimeWindowDays {
			break
		}

		// Expand the window
		timeWindowDays *= expansionFactor
	}

	return filtered
}
