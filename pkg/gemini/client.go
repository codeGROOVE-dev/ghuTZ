// Package gemini provides a client for Google's Gemini AI API.
package gemini

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

// Response represents the Gemini API response structure.
//
//nolint:govet // fieldalignment is a minor optimization, struct clarity is preferred
type Response struct {
	// Place larger alignment fields first (float64 = 8 bytes)
	Latitude  float64 `json:"latitude"`  // GPS latitude coordinate
	Longitude float64 `json:"longitude"` // GPS longitude coordinate
	// Boolean field (1 byte, but usually padded)
	SuspiciousMismatch bool `json:"suspicious_mismatch"` // Flag for suspicious location patterns
	// Strings are pointers (8 bytes each on 64-bit), group them together
	DetectedTimezone   string `json:"detected_timezone"`
	DetectedLocation   string `json:"detected_location"`
	ConfidenceLevel    string `json:"confidence_level"` // "high", "medium", or "low"
	DetectionReasoning string `json:"detection_reasoning"`
	MismatchReason     string `json:"mismatch_reason"` // Explanation if suspicious_mismatch is true
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
func (c *Client) CallWithSDK(ctx context.Context, prompt string, cache CacheInterface, logger Logger) (*Response, error) {
	// Check cache first
	if cachedResponse := c.checkCache(prompt, cache, logger); cachedResponse != nil {
		return cachedResponse, nil
	}

	// Create client
	client, err := c.createClient(ctx, logger)
	if err != nil {
		return nil, err
	}

	// Configure request
	modelName, contents, genConfig := c.configureRequest(prompt, logger)

	// Make API call with retries
	resp, err := c.makeAPICallWithRetry(ctx, client, modelName, contents, genConfig, logger)
	if err != nil {
		return nil, err
	}

	// Process response and cache
	return c.processResponseAndCache(resp, prompt, cache, logger)
}

// checkCache checks for cached responses and returns them if valid.
func (c *Client) checkCache(prompt string, cache CacheInterface, logger Logger) *Response {
	if cache == nil {
		return nil
	}

	cacheKey := fmt.Sprintf("genai:%s:%s", c.model, prompt)
	cachedData, found := cache.APICall(cacheKey, []byte(prompt))
	if !found {
		return nil
	}

	var result Response
	if err := json.Unmarshal(cachedData, &result); err != nil {
		logger.Debug("Failed to unmarshal cached Gemini response", "error", err)
		return nil
	}

	if result.DetectedTimezone == "" {
		logger.Warn("Cached Gemini response is invalid/empty, fetching fresh")
		return nil
	}

	// Validate the cached result has actual data
	logger.Info("Using cached Gemini response", "timezone", result.DetectedTimezone, "confidence", result.ConfidenceLevel)
	return &result
}

// createClient creates and configures the Gemini client.
func (c *Client) createClient(ctx context.Context, logger Logger) (*genai.Client, error) {
	var config *genai.ClientConfig

	if c.apiKey != "" {
		config = &genai.ClientConfig{
			Backend: genai.BackendGeminiAPI,
			APIKey:  c.apiKey,
		}
		logger.Info("Using Gemini API with API key")
	} else {
		projectID := c.getProjectID()
		config = &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  projectID,
			Location: "us-central1",
		}
		logger.Info("Using Vertex AI with Application Default Credentials", "project", projectID, "location", "us-central1")
	}

	client, err := genai.NewClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}
	return client, nil
}

// getProjectID determines the GCP project ID to use.
func (c *Client) getProjectID() string {
	if c.gcpProject != "" {
		return c.gcpProject
	}

	if projectID := os.Getenv("GCP_PROJECT"); projectID != "" {
		return projectID
	}
	if projectID := os.Getenv("GOOGLE_CLOUD_PROJECT"); projectID != "" {
		return projectID
	}
	return "gutz-468911" // Default project for guTZ
}

// configureRequest prepares the model, content, and generation configuration.
func (c *Client) configureRequest(prompt string, logger Logger) (string, []*genai.Content, *genai.GenerateContentConfig) {
	modelName := c.model
	if modelName == "" {
		modelName = "gemini-2.5-flash-lite"
	}
	modelName = strings.TrimPrefix(modelName, "models/")
	logger.Debug("Using model", "model", modelName)

	contents := []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{Text: prompt},
			},
		},
	}

	maxTokens := int32(2500)
	temperature := float32(0.1)

	genConfig := &genai.GenerateContentConfig{
		Temperature:      &temperature,
		MaxOutputTokens:  maxTokens,
		ResponseMIMEType: "application/json",
		ResponseSchema:   c.createResponseSchema(),
	}

	return modelName, contents, genConfig
}

// createResponseSchema creates the JSON schema for structured responses.
func (*Client) createResponseSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"detected_timezone": {
				Type:        genai.TypeString,
				Description: "The most likely timezone for this user in IANA format (e.g., 'America/New_York', 'Europe/London', 'Asia/Tokyo')",
			},
			"confidence_level": {
				Type:        genai.TypeString,
				Enum:        []string{"high", "medium", "low"},
				Description: "Confidence level in the timezone detection: high (strong evidence), medium (reasonable evidence), low (weak evidence)",
			},
			"detected_location": {
				Type: genai.TypeString,
				Description: "The detected location/region that supports the timezone conclusion " +
					"(e.g., 'New York, United States', 'London, United Kingdom')",
			},
			"latitude": {
				Type:        genai.TypeNumber,
				Description: "The latitude coordinate of the most exact location guess you can determine (e.g., 40.7128 for New York). ",
			},
			"longitude": {
				Type:        genai.TypeNumber,
				Description: "The longitude coordinate of the most exact location guess you can determine.",
			},
			"detection_reasoning": {
				Type:        genai.TypeString,
				Description: "Explanation of the key evidence and reasoning that led to this timezone conclusion",
			},
			"suspicious_mismatch": {
				Type:        genai.TypeBoolean,
				Description: "True if the user's claimed location is implausible or incompatible with activity patterns (e.g., claims Antarctica but shows US Eastern timezone activity)",
			},
			"mismatch_reason": {
				Type:        genai.TypeString,
				Description: "If suspicious_mismatch is true, explain the discrepancy (e.g., 'User claims Antarctica, but activity suggests US Eastern timezone'). Empty string if no mismatch.",
			},
		},
		PropertyOrdering: []string{"detected_timezone", "confidence_level", "detected_location", "latitude", "longitude", "detection_reasoning", "suspicious_mismatch", "mismatch_reason"},
		Required:         []string{"detected_timezone", "confidence_level", "detected_location", "latitude", "longitude", "detection_reasoning", "suspicious_mismatch", "mismatch_reason"},
	}
}

// makeAPICallWithRetry executes the API call with retry logic.
func (c *Client) makeAPICallWithRetry(ctx context.Context, client *genai.Client, modelName string,
	contents []*genai.Content, config *genai.GenerateContentConfig, logger Logger,
) (*genai.GenerateContentResponse, error) {
	maxRetries := 3
	baseDelay := 100 * time.Millisecond
	jitter := 50 * time.Millisecond

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := client.Models.GenerateContent(ctx, modelName, contents, config)

		if err == nil {
			return resp, nil
		}

		if attempt == maxRetries {
			return nil, fmt.Errorf("gemini API call failed after %d attempts: %w", maxRetries+1, err)
		}

		if !c.isTransientError(err) {
			logger.Warn("Non-transient Gemini API error - giving up", "error", err)
			return nil, fmt.Errorf("non-transient gemini API error: %w", err)
		}

		delay := baseDelay * time.Duration(1<<uint(attempt))
		jitterBig, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(jitter)))
		if err != nil {
			jitterBig = big.NewInt(0) // Fall back to no jitter on error
		}
		jitterDelay := time.Duration(jitterBig.Int64())
		totalDelay := delay + jitterDelay

		logger.Debug("Retrying Gemini API call", "attempt", attempt+1, "delay_ms", totalDelay.Milliseconds(), "error", err.Error())
		time.Sleep(totalDelay)
	}

	return nil, errors.New("unexpected end of retry loop")
}

// isTransientError determines if an error should trigger a retry.
func (*Client) isTransientError(err error) bool {
	errStr := strings.ToLower(err.Error())
	transientIndicators := []string{
		"rate limit", "quota", "timeout", "deadline", "unavailable",
		"internal server error", "502", "503", "504",
		"error 500", "status: internal", "an internal error has occurred",
	}

	for _, indicator := range transientIndicators {
		if strings.Contains(errStr, indicator) {
			return true
		}
	}
	return false
}

// processResponseAndCache validates response, extracts content, and caches result.
func (c *Client) processResponseAndCache(resp *genai.GenerateContentResponse, prompt string, cache CacheInterface, logger Logger) (*Response, error) {
	if resp == nil || len(resp.Candidates) == 0 {
		return nil, errors.New("empty response from Gemini API")
	}

	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return nil, errors.New("no content in Gemini response")
	}

	text := candidate.Content.Parts[0].Text
	if text == "" {
		return nil, errors.New("empty text in Gemini response")
	}

	logger.Debug("Raw Gemini response", "response_text", text)

	var geminiResp Response
	if err := json.Unmarshal([]byte(text), &geminiResp); err != nil {
		// Fallback to JSON extraction for older cached responses
		logger.Debug("Direct JSON parse failed, trying extraction", "error", err)
		jsonText, extractErr := c.extractJSON(text)
		if extractErr != nil {
			logger.Warn("Failed to parse Gemini JSON response", "parse_error", err, "extract_error", extractErr, "response_text", text)
			return nil, fmt.Errorf("failed to parse Gemini JSON response: %w", err)
		}
		if err := json.Unmarshal([]byte(jsonText), &geminiResp); err != nil {
			logger.Warn("Failed to parse extracted JSON", "error", err, "json_text", jsonText, "response_text", text)
			return nil, fmt.Errorf("failed to parse Gemini JSON response: %w", err)
		}
	}

	c.cleanResponse(&geminiResp)

	// Log the parsed struct values for debugging
	logger.Debug("Parsed Gemini response struct",
		"detected_timezone", geminiResp.DetectedTimezone,
		"detected_location", geminiResp.DetectedLocation,
		"latitude", geminiResp.Latitude,
		"longitude", geminiResp.Longitude,
		"confidence_level", geminiResp.ConfidenceLevel,
		"detection_reasoning", geminiResp.DetectionReasoning)

	// Validate the response has required fields
	if geminiResp.DetectedTimezone == "" {
		logger.Warn("Gemini response missing timezone field", "response", geminiResp)
		return nil, errors.New("gemini response missing timezone information")
	}

	logger.Info("Gemini response received",
		"timezone", geminiResp.DetectedTimezone,
		"location", geminiResp.DetectedLocation,
		"confidence", geminiResp.ConfidenceLevel)

	// Cache the response
	if cache != nil {
		cacheKey := fmt.Sprintf("genai:%s:%s", c.model, prompt)
		if respData, err := json.Marshal(geminiResp); err == nil {
			if err := cache.SetAPICall(cacheKey, []byte(prompt), respData); err != nil {
				logger.Debug("Failed to cache Gemini response", "error", err)
			} else {
				logger.Debug("Cached Gemini response", "timezone", geminiResp.DetectedTimezone)
			}
		}
	}

	return &geminiResp, nil
}

// extractJSON extracts JSON content from a response that may contain explanatory text.
func (*Client) extractJSON(text string) (string, error) {
	// First try to parse as direct JSON
	if isValidJSON(text) {
		return text, nil
	}

	// Look for JSON code blocks (```json ... ```)
	if start := strings.Index(text, "```json"); start != -1 {
		start += 7 // Skip past "```json"
		if end := strings.Index(text[start:], "```"); end != -1 {
			jsonText := strings.TrimSpace(text[start : start+end])
			if isValidJSON(jsonText) {
				return jsonText, nil
			}
		}
	}

	// Look for JSON blocks without language specifier (``` ... ```)
	if start := strings.Index(text, "```"); start != -1 {
		start += 3 // Skip past "```"
		if end := strings.Index(text[start:], "```"); end != -1 {
			jsonText := strings.TrimSpace(text[start : start+end])
			if isValidJSON(jsonText) {
				return jsonText, nil
			}
		}
	}

	// Look for JSON objects starting with { and ending with }
	if start := strings.Index(text, "{"); start != -1 {
		if end := strings.LastIndex(text, "}"); end != -1 && end > start {
			jsonText := strings.TrimSpace(text[start : end+1])
			if isValidJSON(jsonText) {
				return jsonText, nil
			}
		}
	}

	return "", errors.New("no valid JSON found in response")
}

// isValidJSON checks if a string is valid JSON by attempting to parse it.
func isValidJSON(s string) bool {
	var js map[string]any
	return json.Unmarshal([]byte(s), &js) == nil
}

// cleanResponse cleans up response data by removing newlines and extra spaces.
func (*Client) cleanResponse(resp *Response) {
	resp.DetectedTimezone = strings.TrimSpace(strings.ReplaceAll(resp.DetectedTimezone, "\n", " "))
	resp.DetectedLocation = strings.TrimSpace(strings.ReplaceAll(resp.DetectedLocation, "\n", " "))
	resp.DetectionReasoning = strings.TrimSpace(strings.ReplaceAll(resp.DetectionReasoning, "\n", " "))
}
