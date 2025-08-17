package ghutz

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Collaborator represents someone the user interacts with
type Collaborator struct {
	Username       string
	InteractionCount int
	DetectedTimezone string
	Confidence     float64
}

// analyzeCollaboratorTimezones analyzes the timezones of frequent collaborators
func (d *Detector) analyzeCollaboratorTimezones(ctx context.Context, username string, events []PublicEvent, prs []PullRequest, issues []Issue) []Collaborator {
	collaboratorCounts := make(map[string]int)
	
	// Track collaborators from events
	for _, event := range events {
		switch event.Type {
		case "PullRequestEvent", "PullRequestReviewEvent", "PullRequestReviewCommentEvent":
			var payload struct {
				PullRequest struct {
					User struct {
						Login string `json:"login"`
					} `json:"user"`
				} `json:"pull_request"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				if payload.PullRequest.User.Login != "" && payload.PullRequest.User.Login != username {
					collaboratorCounts[payload.PullRequest.User.Login]++
				}
			}
		case "IssuesEvent", "IssueCommentEvent":
			var payload struct {
				Issue struct {
					User struct {
						Login string `json:"login"`
					} `json:"user"`
				} `json:"issue"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				if payload.Issue.User.Login != "" && payload.Issue.User.Login != username {
					collaboratorCounts[payload.Issue.User.Login]++
				}
			}
		}
	}
	
	// Track collaborators from PRs - people who reviewed or commented
	for _, pr := range prs {
		// Extract repository owner
		if pr.Repository != "" {
			parts := strings.Split(pr.Repository, "/")
			if len(parts) == 2 && parts[0] != username {
				collaboratorCounts[parts[0]]++ // Repository owner
			}
		}
	}
	
	// Track collaborators from issues - people who are assignees or reporters
	for _, issue := range issues {
		// Extract repository owner
		if issue.Repository != "" {
			parts := strings.Split(issue.Repository, "/")
			if len(parts) == 2 && parts[0] != username {
				collaboratorCounts[parts[0]]++ // Repository owner
			}
		}
	}
	
	// Sort collaborators by interaction count
	type collabCount struct {
		username string
		count    int
	}
	var sortedCollabs []collabCount
	for user, count := range collaboratorCounts {
		// Filter out bots and low-quality collaborators
		if count >= 5 && !isBot(user) && !isSystemAccount(user) {
			sortedCollabs = append(sortedCollabs, collabCount{user, count})
		}
	}
	sort.Slice(sortedCollabs, func(i, j int) bool {
		return sortedCollabs[i].count > sortedCollabs[j].count
	})
	
	// Analyze top collaborators' timezones (limit to top 3 to minimize API calls)
	var collaborators []Collaborator
	maxCollaborators := 3
	for i, collab := range sortedCollabs {
		if i >= maxCollaborators {
			break
		}
		
		// Try to detect collaborator's timezone using a simplified version
		collabTimezone, confidence := d.detectCollaboratorTimezone(ctx, collab.username)
		if collabTimezone != "" {
			collaborators = append(collaborators, Collaborator{
				Username:         collab.username,
				InteractionCount: collab.count,
				DetectedTimezone: collabTimezone,
				Confidence:       confidence,
			})
		}
	}
	
	return collaborators
}

// isBot checks if a username appears to be a bot
func isBot(username string) bool {
	username = strings.ToLower(username)
	botPatterns := []string{
		"bot", "[bot]", "dependabot", "renovate", "allcontributors", 
		"snyk", "codecov", "greenkeeper", "security", "stale",
		"imgbot", "github-actions", "mergify", "netlify",
	}
	
	for _, pattern := range botPatterns {
		if strings.Contains(username, pattern) {
			return true
		}
	}
	return false
}

// isSystemAccount checks if a username is a system/organization account that shouldn't be analyzed
func isSystemAccount(username string) bool {
	username = strings.ToLower(username)
	systemPatterns := []string{
		"ready-to-review", "auto-", "ci-", "build-", "deploy-",
		"github", "gitlab", "bitbucket", "actions-", "workflow-",
	}
	
	for _, pattern := range systemPatterns {
		if strings.Contains(username, pattern) {
			return true
		}
	}
	return false
}

// detectCollaboratorTimezone performs a simplified timezone detection for a collaborator
func (d *Detector) detectCollaboratorTimezone(ctx context.Context, username string) (string, float64) {
	// Fetch just recent events for the collaborator
	events, err := d.fetchPublicEvents(ctx, username)
	if err != nil || len(events) < 10 {
		return "", 0
	}
	
	// Simple heuristic: look at the hour distribution
	hourCounts := make(map[int]int)
	for _, event := range events {
		hour := event.CreatedAt.UTC().Hour()
		hourCounts[hour]++
	}
	
	// Find quiet hours (likely sleep time)
	minActivity := len(events)
	quietStart := -1
	for hour := 0; hour < 24; hour++ {
		// Check 6-hour window
		windowActivity := 0
		for i := 0; i < 6; i++ {
			h := (hour + i) % 24
			windowActivity += hourCounts[h]
		}
		if windowActivity < minActivity {
			minActivity = windowActivity
			quietStart = hour
		}
	}
	
	// If we found quiet hours, estimate timezone
	if quietStart >= 0 && minActivity < len(events)/4 {
		// Assume quiet hours are 12am-6am local time
		// If quiet starts at UTC hour X, and we expect it to be midnight local,
		// then UTC offset = 0 - X
		// For example, if quiet starts at UTC 5am, offset = 0 - 5 = -5 (Eastern US)
		// If quiet starts at UTC 23pm, offset = 0 - 23 = -23 + 24 = 1 (Central Europe)
		offset := -quietStart
		if offset < -12 {
			offset += 24
		}
		
		timezone := timezoneFromOffset(offset)
		confidence := 0.3 + (float64(len(events))/100.0)*0.3 // 30-60% confidence based on data amount
		if confidence > 0.6 {
			confidence = 0.6
		}
		
		return timezone, confidence
	}
	
	return "", 0
}

// CollaborationPattern represents when the user collaborates with others
type CollaborationPattern struct {
	MorningCollaborations   int // 6am-12pm local
	AfternoonCollaborations int // 12pm-6pm local
	EveningCollaborations   int // 6pm-12am local
	NightCollaborations     int // 12am-6am local
	
	// Timezone distribution of collaborators
	CollaboratorTimezones map[string]int // timezone -> count
}

// analyzeCollaborationPatterns analyzes when and with whom the user collaborates
func analyzeCollaborationPatterns(events []PublicEvent, collaborators []Collaborator, userOffset int) CollaborationPattern {
	pattern := CollaborationPattern{
		CollaboratorTimezones: make(map[string]int),
	}
	
	// Count collaboration times
	for _, event := range events {
		if event.Type == "PullRequestEvent" || event.Type == "IssuesEvent" || 
		   event.Type == "PullRequestReviewEvent" || event.Type == "IssueCommentEvent" {
			// Convert UTC to local time
			localHour := (event.CreatedAt.UTC().Hour() + userOffset + 24) % 24
			
			switch {
			case localHour >= 6 && localHour < 12:
				pattern.MorningCollaborations++
			case localHour >= 12 && localHour < 18:
				pattern.AfternoonCollaborations++
			case localHour >= 18 && localHour < 24:
				pattern.EveningCollaborations++
			default:
				pattern.NightCollaborations++
			}
		}
	}
	
	// Count collaborator timezones
	for _, collab := range collaborators {
		if collab.DetectedTimezone != "" && collab.Confidence > 0.3 {
			pattern.CollaboratorTimezones[collab.DetectedTimezone]++
		}
	}
	
	return pattern
}

// formatCollaborationInsights formats collaboration patterns for display
func formatCollaborationInsights(pattern CollaborationPattern, collaborators []Collaborator) string {
	var insights []string
	
	// Find primary collaboration timezone
	if len(pattern.CollaboratorTimezones) > 0 {
		maxCount := 0
		primaryTZ := ""
		for tz, count := range pattern.CollaboratorTimezones {
			if count > maxCount {
				maxCount = count
				primaryTZ = tz
			}
		}
		if primaryTZ != "" {
			insights = append(insights, fmt.Sprintf("Most collaborators are in %s", primaryTZ))
		}
	}
	
	// Analyze collaboration time patterns
	total := pattern.MorningCollaborations + pattern.AfternoonCollaborations + 
	         pattern.EveningCollaborations + pattern.NightCollaborations
	if total > 0 {
		morningPct := float64(pattern.MorningCollaborations) / float64(total) * 100
		afternoonPct := float64(pattern.AfternoonCollaborations) / float64(total) * 100
		eveningPct := float64(pattern.EveningCollaborations) / float64(total) * 100
		nightPct := float64(pattern.NightCollaborations) / float64(total) * 100
		
		// Find peak collaboration time
		peak := "morning"
		peakPct := morningPct
		if afternoonPct > peakPct {
			peak = "afternoon"
			peakPct = afternoonPct
		}
		if eveningPct > peakPct {
			peak = "evening"
			peakPct = eveningPct
		}
		if nightPct > peakPct {
			peak = "night"
			peakPct = nightPct
		}
		
		if peakPct > 40 {
			insights = append(insights, fmt.Sprintf("Peak collaboration time: %s (%.0f%%)", peak, peakPct))
		}
		
		// Check for unusual patterns
		if nightPct > 20 {
			insights = append(insights, "Significant night-time collaboration (possible cross-timezone work)")
		}
	}
	
	// List top collaborators with timezones
	if len(collaborators) > 0 {
		var collabInfo []string
		for i, collab := range collaborators {
			if i >= 3 {
				break
			}
			if collab.DetectedTimezone != "" {
				collabInfo = append(collabInfo, fmt.Sprintf("%s (%s)", collab.Username, collab.DetectedTimezone))
			}
		}
		if len(collabInfo) > 0 {
			insights = append(insights, "Top collaborators: "+strings.Join(collabInfo, ", "))
		}
	}
	
	if len(insights) > 0 {
		return strings.Join(insights, "\n")
	}
	return ""
}