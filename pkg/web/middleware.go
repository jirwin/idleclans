package web

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

type contextKey string

const sessionContextKey contextKey = "session"

// withAuth wraps a handler with authentication check
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			s.logger.Debug("No session cookie")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		session, ok := s.sessionStore.Get(cookie.Value)
		if !ok {
			s.logger.Debug("Invalid or expired session")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Add session to context
		ctx := context.WithValue(r.Context(), sessionContextKey, session)
		next(w, r.WithContext(ctx))
	}
}

// getSession retrieves the session from context
func getSession(r *http.Request) *Session {
	session, ok := r.Context().Value(sessionContextKey).(*Session)
	if !ok {
		return nil
	}
	return session
}

// corsMiddleware adds CORS headers for development
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.config.BaseURL)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs HTTP requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.logger.Debug("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote", r.RemoteAddr),
		)
		next.ServeHTTP(w, r)
	})
}

