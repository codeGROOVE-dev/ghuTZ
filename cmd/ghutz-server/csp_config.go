// Package main implements the ghutz web server for GitHub user timezone detection.
package main

import (
	"fmt"
	"os"
	"strings"
)

// cspPolicy returns a unified Content Security Policy for all environments.
func cspPolicy() string {
	var directives []string

	// Base policy
	directives = append(directives, "default-src 'self'")

	// Script sources
	scriptSrcs := []string{
		"'self'",
		"https://unpkg.com",
		"https://cdn.jsdelivr.net", // Backup CDN
		"'sha256-0L8agW2YLS+EfMeklN/dgQMfpkz0suPAsIeUdcp1R9g='", // Hash for inline script
		"'sha256-JZMz+uJrH5K6MAxLEVyQACtiYTnjpUAvKBzQwbkVSBs='", // Another inline script hash
	}

	directives = append(directives, fmt.Sprintf("script-src %s", strings.Join(scriptSrcs, " ")))

	// Style sources - unfortunately need unsafe-inline for Leaflet
	styleSrcs := []string{
		"'self'",
		"'unsafe-inline'", // Required for Leaflet and inline styles
		"https://unpkg.com",
		"https://cdn.jsdelivr.net",
		"https://fonts.googleapis.com",
	}
	directives = append(directives,
		fmt.Sprintf("style-src %s", strings.Join(styleSrcs, " ")),
		fmt.Sprintf("style-src-elem %s", strings.Join(styleSrcs, " ")))

	// Image sources
	imgSrcs := []string{
		"'self'",
		"data:", // For inline images
		"https://*.tile.openstreetmap.org",
		"https://a.tile.openstreetmap.org",
		"https://b.tile.openstreetmap.org",
		"https://c.tile.openstreetmap.org",
		"https://unpkg.com",
	}
	directives = append(directives, fmt.Sprintf("img-src %s", strings.Join(imgSrcs, " ")))

	// Font sources
	fontSrcs := []string{
		"'self'",
		"https://fonts.gstatic.com",
		"data:", // For inline fonts
	}
	directives = append(directives, fmt.Sprintf("font-src %s", strings.Join(fontSrcs, " ")))

	// Connect sources (XHR, fetch, WebSockets)
	connectSrcs := []string{
		"'self'",
		"https://*.tile.openstreetmap.org",
		"https://a.tile.openstreetmap.org",
		"https://b.tile.openstreetmap.org",
		"https://c.tile.openstreetmap.org",
	}
	directives = append(directives,
		fmt.Sprintf("connect-src %s", strings.Join(connectSrcs, " ")),
		"frame-src https://www.openstreetmap.org",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"child-src 'self'",
		"media-src 'none'",
	)

	// Upgrade insecure requests if explicitly in production
	if os.Getenv("PRODUCTION") == "true" {
		directives = append(directives, "upgrade-insecure-requests")
	}

	return strings.Join(directives, "; ")
}
