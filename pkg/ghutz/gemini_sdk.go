package ghutz

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/retry"
	"google.golang.org/genai"
)

// callGeminiWithSDK calls the Gemini API using the official SDK.
func (d *Detector) callGeminiWithSDK(ctx context.Context, prompt string, verbose bool) (*geminiResponse, error) {
	// Check cache first if available
	cacheKey := fmt.Sprintf("genai:%s:%s", d.geminiModel, prompt)
	if d.cache != nil {
		if cachedData, found := d.cache.APICall(cacheKey, []byte(prompt)); found {
			d.logger.Debug("Gemini SDK cache hit", "cache_data_length", len(cachedData))
			var result geminiResponse
			if err := json.Unmarshal(cachedData, &result); err != nil {
				d.logger.Debug("Failed to unmarshal cached Gemini response", "error", err)
			} else if result.DetectedTimezone != "" || result.Timezone != "" {
				// Validate the cached result has actual data (check both new and old format)
				tz := result.DetectedTimezone
				if tz == "" {
					tz = result.Timezone
				}
				d.logger.Debug("Using cached Gemini response",
					"timezone", tz,
					"confidence", result.ConfidenceLevel)
				return &result, nil
			} else {
				d.logger.Warn("Cached Gemini response is invalid/empty, fetching fresh")
				// Continue to make a fresh API call
			}
		}
	}

	// Create client based on authentication method
	var client *genai.Client
	var err error
	var config *genai.ClientConfig

	if d.geminiAPIKey != "" {
		// When using API key, use Gemini API backend (not Vertex AI)
		// API keys work with Gemini API, not Vertex AI
		config = &genai.ClientConfig{
			Backend: genai.BackendGeminiAPI,
			APIKey:  d.geminiAPIKey,
		}
		d.logger.Info("Using Gemini API with API key")
	} else {
		// When using ADC, use Vertex AI backend
		projectID := d.gcpProject
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
		d.logger.Info("Using Vertex AI with Application Default Credentials", "project", projectID, "location", "us-central1")
	}

	client, err = genai.NewClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	// Select model
	modelName := d.geminiModel
	if modelName == "" {
		modelName = "gemini-2.5-flash-lite"
	}

	// Vertex AI expects the model name without "models/" prefix
	modelName = strings.TrimPrefix(modelName, "models/")

	d.logger.Debug("Using model", "model", modelName)

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
					d.logger.Warn("Gemini API transient error, retrying", "error", genErr)
					return genErr // Retry
				}
				// For non-transient errors, don't retry
				d.logger.Error("Gemini API non-transient error", "error", genErr)
				return retry.Unrecoverable(genErr)
			}
			return nil
		},
		retry.Attempts(3),
		retry.Delay(time.Second*2),
		retry.MaxDelay(time.Second*10),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(n uint, err error) {
			d.logger.Debug("Retrying Gemini API call", "attempt", n+1, "error", err)
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("Gemini API call failed after retries: %w", err)
	}

	// Extract text from response
	if resp == nil {
		d.logger.Error("Gemini API returned nil response")
		return nil, fmt.Errorf("nil response from Gemini API")
	}
	
	if len(resp.Candidates) == 0 {
		d.logger.Error("Gemini API returned no candidates", 
			"usage", resp.UsageMetadata,
			"model", modelName)
		return nil, fmt.Errorf("no candidates in Gemini API response")
	}

	candidate := resp.Candidates[0]
	
	// Log candidate details
	d.logger.Debug("Gemini candidate details",
		"finish_reason", candidate.FinishReason,
		"safety_ratings", candidate.SafetyRatings,
		"content_parts_count", len(candidate.Content.Parts))
	
	if candidate.Content == nil {
		d.logger.Error("Gemini candidate has nil content")
		return nil, fmt.Errorf("nil content in Gemini response")
	}
	
	if len(candidate.Content.Parts) == 0 {
		d.logger.Error("Gemini candidate has no content parts",
			"finish_reason", candidate.FinishReason)
		return nil, fmt.Errorf("empty response from Gemini API")
	}

	// Get the JSON response
	jsonText := ""
	for i, part := range candidate.Content.Parts {
		d.logger.Debug("Examining part", "index", i, "has_text", part.Text != "")
		if part.Text != "" {
			jsonText = part.Text
			d.logger.Debug("Found text in part", "index", i, "text_length", len(part.Text))
			break
		}
	}

	if jsonText == "" {
		return nil, fmt.Errorf("no text in Gemini response")
	}

	// Always log raw response for debugging (truncate if too long)
	responsePreview := jsonText
	if len(responsePreview) > 500 {
		responsePreview = responsePreview[:500] + "..."
	}
	d.logger.Debug("Gemini raw response", "response_preview", responsePreview, "full_length", len(jsonText))

	// Parse the JSON response
	var geminiResp geminiResponse
	if err := json.Unmarshal([]byte(jsonText), &geminiResp); err != nil {
		d.logger.Warn("failed to parse Gemini JSON response", "error", err, "raw", jsonText)
		return nil, fmt.Errorf("failed to parse Gemini response: %w", err)
	}
	
	// Always log the parsed response for debugging
	d.logger.Debug("Parsed Gemini response", 
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
	if d.cache != nil && tz != "" {
		responseJSON, err := json.Marshal(geminiResp)
		if err == nil {
			if err := d.cache.SetAPICall(cacheKey, []byte(prompt), responseJSON); err != nil {
				d.logger.Debug("Failed to cache Gemini response", "error", err)
			} else {
				d.logger.Debug("Cached Gemini response", "timezone", tz)
			}
		}
	}

	return &geminiResp, nil
}