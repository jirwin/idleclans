package web

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
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

	// For index.html, inject admin mode flag
	if path == "/index.html" && isAdmin {
		// Inject a script tag that sets the admin mode flag before the app loads
		adminScript := `<script>window.__ADMIN_MODE__=true;</script>`
		contentStr := string(content)
		// Insert before the closing </head> tag
		contentStr = strings.Replace(contentStr, "</head>", adminScript+"</head>", 1)
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

