package web

import (
	"context"
	"embed"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed static/*
var staticFiles embed.FS

// handleStaticFiles serves the React SPA static files for public frontend
func (s *Server) handleStaticFiles(w http.ResponseWriter, r *http.Request) {
	s.serveStaticFile(w, r, false)
}

// handleAdminStaticFiles serves the React SPA static files for admin frontend
func (s *Server) handleAdminStaticFiles(w http.ResponseWriter, r *http.Request) {
	s.serveStaticFile(w, r, true)
}

func (s *Server) serveStaticFile(w http.ResponseWriter, r *http.Request, isAdmin bool) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	// Security: prevent path traversal
	path = filepath.Clean(path)
	if strings.Contains(path, "..") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Try to serve from embedded files
	fsPath := "static" + path
	
	content, err := staticFiles.ReadFile(fsPath)
	if err != nil {
		// For SPA routing, serve index.html for non-file paths
		if !strings.Contains(path, ".") {
			content, err = staticFiles.ReadFile("static/index.html")
			if err != nil {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			path = "/index.html"
		} else {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
	}

	// For index.html, inject Open Graph meta tags for market items
	if path == "/index.html" {
		contentStr := string(content)
		
		// Check if this is a market item page (for Discord unfurling)
		if r.URL.Path == "/market" || strings.HasPrefix(r.URL.Path, "/market") {
			if itemIDStr := r.URL.Query().Get("item"); itemIDStr != "" {
				ogTags := s.generateMarketItemOGTags(r.Context(), itemIDStr)
				if ogTags != "" {
					contentStr = strings.Replace(contentStr, "</head>", ogTags+"</head>", 1)
				}
			}
		}
		
		// Inject admin mode flag if needed
		if isAdmin {
			adminScript := `<script>window.__ADMIN_MODE__=true;</script>`
			contentStr = strings.Replace(contentStr, "</head>", adminScript+"</head>", 1)
		}
		
		content = []byte(contentStr)
	}

	// Set content type based on file extension
	contentType := getContentType(path)
	w.Header().Set("Content-Type", contentType)

	// Set cache headers for static assets
	if strings.HasPrefix(path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}

	w.Write(content)
}

// generateMarketItemOGTags generates Open Graph meta tags for a market item
func (s *Server) generateMarketItemOGTags(ctx context.Context, itemIDStr string) string {
	if s.marketDB == nil {
		return ""
	}

	itemID, err := strconv.Atoi(itemIDStr)
	if err != nil {
		return ""
	}

	// Get item info
	item, err := s.marketDB.GetItem(ctx, itemID)
	if err != nil || item == nil {
		return ""
	}

	// Get current price
	currentPrice, _ := s.marketDB.GetLatestPrice(ctx, itemID)

	// Build description
	var description string
	if currentPrice != nil {
		sellPrice := formatGoldForOG(currentPrice.LowestSellPrice)
		buyPrice := formatGoldForOG(currentPrice.HighestBuyPrice)
		description = fmt.Sprintf("Sell: %s gold | Buy: %s gold", sellPrice, buyPrice)
		if currentPrice.HighestBuyPrice > 0 {
			spread := currentPrice.LowestSellPrice - currentPrice.HighestBuyPrice
			spreadPct := float64(spread) / float64(currentPrice.HighestBuyPrice) * 100
			description += fmt.Sprintf(" | Spread: %.1f%%", spreadPct)
		}
	} else {
		description = "View price history and market data"
	}

	escapedName := html.EscapeString(item.DisplayName)
	escapedDesc := html.EscapeString(description)
	baseURL := s.config.BaseURL
	if baseURL == "" {
		baseURL = "https://idleclans.jirwin.dev"
	}
	
	ogImageURL := fmt.Sprintf("%s/api/market/items/%d/og-image.png", baseURL, itemID)
	pageURL := fmt.Sprintf("%s/market?item=%d", baseURL, itemID)

	return fmt.Sprintf(`
    <!-- Open Graph / Discord Unfurl -->
    <meta property="og:type" content="website" />
    <meta property="og:title" content="%s - IdleClans Market" />
    <meta property="og:description" content="%s" />
    <meta property="og:image" content="%s" />
    <meta property="og:image:width" content="800" />
    <meta property="og:image:height" content="320" />
    <meta property="og:url" content="%s" />
    <meta property="og:site_name" content="IdleClans Market" />
    <meta name="twitter:card" content="summary_large_image" />
    <meta name="twitter:title" content="%s - IdleClans Market" />
    <meta name="twitter:description" content="%s" />
    <meta name="twitter:image" content="%s" />
    <meta name="theme-color" content="#10b981" />
    `, escapedName, escapedDesc, ogImageURL, pageURL, escapedName, escapedDesc, ogImageURL)
}

// formatGoldForOG formats gold value for Open Graph description
func formatGoldForOG(value int) string {
	if value >= 1000000000 {
		return fmt.Sprintf("%.1fB", float64(value)/1000000000)
	}
	if value >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(value)/1000000)
	}
	if value >= 1000 {
		return fmt.Sprintf("%.1fK", float64(value)/1000)
	}
	return fmt.Sprintf("%d", value)
}

func getContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	default:
		return "application/octet-stream"
	}
}

// getStaticFS returns a filesystem for serving static files
// This is useful for development when you want to serve from disk
func getStaticFS() fs.FS {
	// Check if we're in development mode
	if devPath := os.Getenv("WEB_DEV_PATH"); devPath != "" {
		return os.DirFS(devPath)
	}

	// Use embedded files
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil
	}
	return sub
}

