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
			} else if result.Timezone != "" {
				// Validate the cached result has actual data
				d.logger.Debug("Using cached Gemini response",
					"timezone", result.Timezone,
					"confidence", result.Confidence)
				return &result, nil
			} else {
				d.logger.Warn("Cached Gemini response is invalid/empty, fetching fresh",
					"timezone", result.Timezone)
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
	maxTokens := int32(100)
	if verbose {
		maxTokens = 300
	}

	temperature := float32(0.1)
	genConfig := &genai.GenerateContentConfig{
		Temperature:      &temperature,
		MaxOutputTokens:  maxTokens,
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"timezone": {
					Type:        genai.TypeString,
					Description: "IANA timezone identifier",
				},
				"offset_utc": {
					Type:        genai.TypeString,
					Description: "UTC offset like UTC-5 or UTC+1",
				},
				"confidence": {
					Type:        genai.TypeNumber,
					Description: "Confidence score between 0 and 1",
				},
				"location_source": {
					Type:        genai.TypeString,
					Description: "Source of location determination",
				},
				"dst": {
					Type:        genai.TypeString,
					Description: "Daylight saving time observation status",
				},
				"error": {
					Type:        genai.TypeString,
					Description: "Error message if timezone cannot be determined",
				},
			},
			Required: []string{"timezone", "offset_utc", "confidence"},
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
	if resp == nil || len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("no response from Gemini API")
	}

	candidate := resp.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from Gemini API")
	}

	// Get the JSON response
	jsonText := ""
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			jsonText = part.Text
			break
		}
	}

	if jsonText == "" {
		return nil, fmt.Errorf("no text in Gemini response")
	}

	if verbose {
		d.logger.Debug("Gemini raw response", "response", jsonText)
	}

	// Parse the JSON response
	var geminiResp geminiResponse
	if err := json.Unmarshal([]byte(jsonText), &geminiResp); err != nil {
		d.logger.Warn("failed to parse Gemini JSON response", "error", err, "raw", jsonText)
		return nil, fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	// Clean up timezone if needed
	geminiResp.Timezone = strings.TrimSpace(geminiResp.Timezone)
	geminiResp.OffsetUTC = strings.TrimSpace(geminiResp.OffsetUTC)

	// Cache the successful response if available
	if d.cache != nil && geminiResp.Timezone != "" {
		responseJSON, err := json.Marshal(geminiResp)
		if err == nil {
			if err := d.cache.SetAPICall(cacheKey, []byte(prompt), responseJSON); err != nil {
				d.logger.Debug("Failed to cache Gemini response", "error", err)
			} else {
				d.logger.Debug("Cached Gemini response", "timezone", geminiResp.Timezone)
			}
		}
	}

	return &geminiResp, nil
}