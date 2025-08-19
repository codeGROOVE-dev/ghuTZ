// Package gemini provides a client for Google's Gemini AI API.
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/retry"
	"google.golang.org/genai"
)

// Response represents the Gemini API response structure.
type Response struct {
	DetectedTimezone   string `json:"detected_timezone"`
	DetectedLocation   string `json:"detected_location"`
	ConfidenceLevel    string `json:"confidence_level"` // "high", "medium", or "low"
	DetectionReasoning string `json:"detection_reasoning"`

	// Fallback fields for old format (deprecated)
	Timezone       string      `json:"timezone,omitempty"`
	Location       string      `json:"location,omitempty"`
	LocationSource string      `json:"location_source,omitempty"`
	Confidence     interface{} `json:"confidence,omitempty"`
	Reasoning      string      `json:"reasoning,omitempty"`
}

// Client represents a Gemini API client.
type Client struct {
	apiKey     string
	model      string
	gcpProject string
}

// NewClient creates a new Gemini API client.
func NewClient(apiKey, model, gcpProject string) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      model,
		gcpProject: gcpProject,
	}
}

// CallWithSDK calls the Gemini API using the official SDK.
func (c *Client) CallWithSDK(ctx context.Context, prompt string, verbose bool, cache CacheInterface, logger Logger) (*Response, error) {
	// Check cache first if available
	cacheKey := fmt.Sprintf("genai:%s:%s", c.model, prompt)
	if cache != nil {
		if cachedData, found := cache.APICall(cacheKey, []byte(prompt)); found {
			logger.Debug("Gemini SDK cache hit", "cache_data_length", len(cachedData))
			var result Response
			if err := json.Unmarshal(cachedData, &result); err != nil {
				logger.Debug("Failed to unmarshal cached Gemini response", "error", err)
			} else if result.DetectedTimezone != "" || result.Timezone != "" {
				// Validate the cached result has actual data (check both new and old format)
				tz := result.DetectedTimezone
				if tz == "" {
					tz = result.Timezone
				}
				logger.Debug("Using cached Gemini response",
					"timezone", tz,
					"confidence", result.ConfidenceLevel)
				return &result, nil
			} else {
				logger.Warn("Cached Gemini response is invalid/empty, fetching fresh")
				// Continue to make a fresh API call
			}
		}
	}

	// Create client based on authentication method
	var client *genai.Client
	var err error
	var config *genai.ClientConfig

	if c.apiKey != "" {
		// When using API key, use Gemini API backend (not Vertex AI)
		// API keys work with Gemini API, not Vertex AI
		config = &genai.ClientConfig{
			Backend: genai.BackendGeminiAPI,
			APIKey:  c.apiKey,
		}
		logger.Info("Using Gemini API with API key")
	} else {
		// When using ADC, use Vertex AI backend
		projectID := c.gcpProject
		if projectID == "" {
			// Try to get from environment
			projectID = os.Getenv("GCP_PROJECT")
			if projectID == "" {
				projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
			}
			if projectID == "" {
				// Default project for ghuTZ
				projectID = "ghutz-468911"
			}
		}

		config = &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  projectID,
			Location: "us-central1",
		}
		logger.Info("Using Vertex AI with Application Default Credentials", "project", projectID, "location", "us-central1")
	}

	client, err = genai.NewClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	// Select model
	modelName := c.model
	if modelName == "" {
		modelName = "gemini-2.5-flash-lite"
	}

	// Vertex AI expects the model name without "models/" prefix
	modelName = strings.TrimPrefix(modelName, "models/")

	logger.Debug("Using model", "model", modelName)

	// Prepare content with user role
	contents := []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{Text: prompt},
			},
		},
	}

	// Configure generation
	maxTokens := int32(2000) // Increased to prevent truncation for pro models
	if verbose {
		maxTokens = 2500
	}

	temperature := float32(0.1)

	// Pro models require thinking, others can have 0
	// -1 enables dynamic thinking for pro models
	var thinkingBudget int32
	if strings.Contains(modelName, "pro") {
		thinkingBudget = -1 // Dynamic thinking for pro models
	} else {
		thinkingBudget = 0 // Disable thinking for faster responses on other models
	}

	genConfig := &genai.GenerateContentConfig{
		Temperature:      &temperature,
		MaxOutputTokens:  maxTokens,
		ResponseMIMEType: "application/json",
		ThinkingConfig: &genai.ThinkingConfig{
			IncludeThoughts: false,
			ThinkingBudget:  &thinkingBudget,
		},
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"detected_timezone": {
					Type:        genai.TypeString,
					Description: "IANA timezone identifier like America/New_York",
				},
				"detected_location": {
					Type:        genai.TypeString,
					Description: "City and region like 'New York, New York' or 'Denver, Colorado'",
				},
				"confidence_level": {
					Type:        genai.TypeString,
					Description: "Confidence level: exactly one of 'high', 'medium', or 'low'",
				},
				"detection_reasoning": {
					Type:        genai.TypeString,
					Description: "2-3 sentences explaining the detection logic and key evidence",
				},
			},
			Required: []string{"detected_timezone", "detected_location", "confidence_level", "detection_reasoning"},
		},
	}

	// Generate content with retry logic for transient errors
	var resp *genai.GenerateContentResponse
	err = retry.Do(
		func() error {
			var genErr error
			resp, genErr = client.Models.GenerateContent(ctx, modelName, contents, genConfig)
			if genErr != nil {
				// Check if it's a context timeout or similar transient error
				if strings.Contains(genErr.Error(), "context deadline exceeded") ||
					strings.Contains(genErr.Error(), "timeout") ||
					strings.Contains(genErr.Error(), "temporary failure") ||
					strings.Contains(genErr.Error(), "503") ||
					strings.Contains(genErr.Error(), "502") ||
					strings.Contains(genErr.Error(), "500") {
					logger.Warn("Gemini API transient error, retrying", "error", genErr)
					return genErr // Retry
				}
				// For non-transient errors, don't retry
				logger.Error("Gemini API non-transient error", "error", genErr)
				return retry.Unrecoverable(genErr)
			}
			return nil
		},
		retry.Attempts(5),
		retry.Delay(time.Second),
		retry.MaxDelay(2*time.Minute),
		retry.DelayType(retry.FullJitterBackoffDelay),
		retry.OnRetry(func(n uint, err error) {
			logger.Debug("Retrying Gemini API call", "attempt", n+1, "error", err)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("gemini API call failed after retries: %w", err)
	}

	// Extract text from response
	if resp == nil {
		logger.Error("Gemini API returned nil response")
		return nil, errors.New("nil response from Gemini API")
	}

	if len(resp.Candidates) == 0 {
		logger.Error("Gemini API returned no candidates",
			"usage", resp.UsageMetadata,
			"model", modelName)
		return nil, errors.New("no candidates in Gemini API response")
	}

	candidate := resp.Candidates[0]

	// Log candidate details
	logger.Debug("Gemini candidate details",
		"finish_reason", candidate.FinishReason,
		"safety_ratings", candidate.SafetyRatings,
		"content_parts_count", len(candidate.Content.Parts))

	if candidate.Content == nil {
		logger.Error("Gemini candidate has nil content")
		return nil, errors.New("nil content in Gemini response")
	}

	if len(candidate.Content.Parts) == 0 {
		logger.Error("Gemini candidate has no content parts",
			"finish_reason", candidate.FinishReason)
		return nil, errors.New("empty response from Gemini API")
	}

	// Get the JSON response
	jsonText := ""
	for i, part := range candidate.Content.Parts {
		logger.Debug("Examining part", "index", i, "has_text", part.Text != "")
		if part.Text != "" {
			jsonText = part.Text
			logger.Debug("Found text in part", "index", i, "text_length", len(part.Text))
			break
		}
	}

	if jsonText == "" {
		return nil, errors.New("no text in Gemini response")
	}

	// Always log raw response for debugging (truncate if too long)
	responsePreview := jsonText
	if len(responsePreview) > 500 {
		responsePreview = responsePreview[:500] + "..."
	}
	logger.Debug("Gemini raw response", "response_preview", responsePreview, "full_length", len(jsonText))

	// Parse the JSON response
	var geminiResp Response
	if err := json.Unmarshal([]byte(jsonText), &geminiResp); err != nil {
		logger.Warn("failed to parse Gemini JSON response", "error", err, "raw", jsonText)
		return nil, fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	// Always log the parsed response for debugging
	logger.Debug("Parsed Gemini response",
		"timezone", geminiResp.DetectedTimezone,
		"location", geminiResp.DetectedLocation,
		"confidence", geminiResp.ConfidenceLevel,
		"reasoning_length", len(geminiResp.DetectionReasoning))

	// Clean up timezone if needed
	geminiResp.DetectedTimezone = strings.TrimSpace(geminiResp.DetectedTimezone)
	geminiResp.DetectedLocation = strings.TrimSpace(geminiResp.DetectedLocation)

	// Cache the successful response if available
	tz := geminiResp.DetectedTimezone
	if tz == "" {
		tz = geminiResp.Timezone // Fallback for old format
	}
	if cache != nil && tz != "" {
		responseJSON, err := json.Marshal(geminiResp)
		if err == nil {
			if err := cache.SetAPICall(cacheKey, []byte(prompt), responseJSON); err != nil {
				logger.Debug("Failed to cache Gemini response", "error", err)
			} else {
				logger.Debug("Cached Gemini response", "timezone", tz)
			}
		}
	}

	return &geminiResp, nil
}
