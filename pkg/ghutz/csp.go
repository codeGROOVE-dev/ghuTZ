package ghutz

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// CSPConfig holds Content Security Policy configuration
type CSPConfig struct {
	// Mode can be "enforce" or "report" (for testing)
	Mode string

	// Nonce for inline scripts/styles (regenerated per request)
	Nonce string

	// Development mode - more permissive for debugging
	DevMode bool

	// Trusted domains for various resource types
	TrustedScriptSrcs  []string
	TrustedStyleSrcs   []string
	TrustedImageSrcs   []string
	TrustedFontSrcs    []string
	TrustedConnectSrcs []string
	TrustedFrameSrcs   []string

	// Report URI for CSP violations
	ReportURI string
}

// NewCSPConfig creates a new CSP configuration with secure defaults
func NewCSPConfig(devMode bool) *CSPConfig {
	return &CSPConfig{
		Mode:    "enforce",
		DevMode: devMode,

		// Default trusted sources
		TrustedScriptSrcs: []string{
			"https://unpkg.com",
			"https://cdn.jsdelivr.net", // Alternative CDN
		},
		TrustedStyleSrcs: []string{
			"https://unpkg.com",
			"https://cdn.jsdelivr.net",
			"https://fonts.googleapis.com",
		},
		TrustedImageSrcs: []string{
			"https://*.tile.openstreetmap.org",
			"https://unpkg.com",
			"data:", // For inline images
		},
		TrustedFontSrcs: []string{
			"https://fonts.gstatic.com",
		},
		TrustedConnectSrcs: []string{
			"https://*.tile.openstreetmap.org",
		},
		TrustedFrameSrcs: []string{
			"https://www.openstreetmap.org",
		},
	}
}

// GenerateNonce creates a cryptographically secure nonce for this request
func (c *CSPConfig) GenerateNonce() error {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("generating CSP nonce: %w", err)
	}
	c.Nonce = base64.StdEncoding.EncodeToString(nonceBytes)
	return nil
}

// BuildPolicy constructs the CSP header value
func (c *CSPConfig) BuildPolicy() string {
	var directives []string

	// Default source - restrictive by default
	directives = append(directives, "default-src 'self'")

	// Script sources
	scriptSrcs := []string{"'self'"}
	if c.Nonce != "" {
		scriptSrcs = append(scriptSrcs, fmt.Sprintf("'nonce-%s'", c.Nonce))
	}
	if c.DevMode {
		// In dev mode, allow eval for better debugging
		scriptSrcs = append(scriptSrcs, "'unsafe-eval'")
	}
	scriptSrcs = append(scriptSrcs, c.TrustedScriptSrcs...)
	directives = append(directives, fmt.Sprintf("script-src %s", strings.Join(scriptSrcs, " ")))

	// Style sources
	styleSrcs := []string{"'self'"}
	if c.Nonce != "" {
		styleSrcs = append(styleSrcs, fmt.Sprintf("'nonce-%s'", c.Nonce))
	}
	// Unfortunately, Leaflet and some other libraries require unsafe-inline for styles
	// We can gradually move away from this by using nonces where possible
	styleSrcs = append(styleSrcs, "'unsafe-inline'")
	styleSrcs = append(styleSrcs, c.TrustedStyleSrcs...)
	directives = append(directives, fmt.Sprintf("style-src %s", strings.Join(styleSrcs, " ")))
	directives = append(directives, fmt.Sprintf("style-src-elem %s", strings.Join(styleSrcs, " ")))

	// Image sources
	imgSrcs := append([]string{"'self'"}, c.TrustedImageSrcs...)
	directives = append(directives, fmt.Sprintf("img-src %s", strings.Join(imgSrcs, " ")))

	// Font sources
	fontSrcs := append([]string{"'self'"}, c.TrustedFontSrcs...)
	directives = append(directives, fmt.Sprintf("font-src %s", strings.Join(fontSrcs, " ")))

	// Connect sources (XHR, WebSockets, etc.)
	connectSrcs := append([]string{"'self'"}, c.TrustedConnectSrcs...)
	directives = append(directives, fmt.Sprintf("connect-src %s", strings.Join(connectSrcs, " ")))

	// Frame sources
	if len(c.TrustedFrameSrcs) > 0 {
		frameSrcs := strings.Join(c.TrustedFrameSrcs, " ")
		directives = append(directives, fmt.Sprintf("frame-src %s", frameSrcs))
	} else {
		directives = append(directives, "frame-src 'none'")
	}

	// Security restrictions
	directives = append(directives, "object-src 'none'")
	directives = append(directives, "base-uri 'self'")
	directives = append(directives, "form-action 'self'")

	// Upgrade insecure requests in production
	if !c.DevMode {
		directives = append(directives, "upgrade-insecure-requests")
	}

	// Reporting
	if c.ReportURI != "" {
		directives = append(directives, fmt.Sprintf("report-uri %s", c.ReportURI))
	}

	return strings.Join(directives, "; ")
}

// HeaderName returns the appropriate CSP header name based on mode
func (c *CSPConfig) HeaderName() string {
	if c.Mode == "report" {
		return "Content-Security-Policy-Report-Only"
	}
	return "Content-Security-Policy"
}

// CSPMiddleware creates a middleware that applies CSP headers
func CSPMiddleware(config *CSPConfig) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Generate a fresh nonce for this request
			if err := config.GenerateNonce(); err != nil {
				// Log error but continue - CSP will work without nonce
				// In production, you'd want to log this properly
			}

			// Build and set the CSP header
			policy := config.BuildPolicy()
			w.Header().Set(config.HeaderName(), policy)

			// Pass the nonce to the handler via context if needed
			// This allows templates to use the nonce for inline scripts
			if config.Nonce != "" {
				ctx := context.WithValue(r.Context(), "csp-nonce", config.Nonce)
				r = r.WithContext(ctx)
			}

			next(w, r)
		}
	}
}

// NonceFromContext extracts the CSP nonce from the request context
func NonceFromContext(ctx context.Context) string {
	if nonce, ok := ctx.Value("csp-nonce").(string); ok {
		return nonce
	}
	return ""
}
