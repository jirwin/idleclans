package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jirwin/idleclans/pkg/idleclans"
	"github.com/jirwin/idleclans/pkg/openai"
	"github.com/jirwin/idleclans/pkg/quests"
	"go.uber.org/zap"
)

// Config holds the web server configuration
type Config struct {
	PublicPort          int
	AdminPort           int
	BaseURL             string
	DiscordClientID     string
	DiscordClientSecret string
	SessionSecret       string
	RequiredGuild       string // Guild name required for registration
	DiscordChannelID    string // Channel to send messages to
	OpenAIAPIKey        string // OpenAI API key for image analysis
	OpenAIModel         string // Vision model for image analysis (e.g., gpt-4o)
}

// DiscordEmbed represents a Discord embed for the web server
type DiscordEmbed struct {
	Title       string
	Description string
	Color       int
	Fields      []DiscordEmbedField
}

// DiscordEmbedField represents a field in a Discord embed
type DiscordEmbedField struct {
	Name   string
	Value  string
	Inline bool
}

// DiscordMessageSender interface for sending messages to Discord
type DiscordMessageSender interface {
	SendMessage(channelID, message string) error
	SendMessageWithEmbed(channelID, content string, embed *DiscordEmbed) error
}

// Server represents the web server
type Server struct {
	config            *Config
	db                *quests.DB
	logger            *zap.Logger
	publicServer      *http.Server
	adminServer       *http.Server
	sessionStore      *SessionStore
	sseBroker         *SSEBroker
	icClient          *idleclans.Client
	discordSender     DiscordMessageSender
	openaiClient      *openai.Client
	keyReferenceImages *KeyReferenceImages
}

// SetDiscordSender sets the Discord message sender
func (s *Server) SetDiscordSender(sender DiscordMessageSender) {
	s.discordSender = sender
}

// NewServer creates a new web server
func NewServer(config *Config, db *quests.DB, logger *zap.Logger) (*Server, error) {
	if config.SessionSecret == "" {
		config.SessionSecret = "default-secret-change-in-production"
	}

	s := &Server{
		config:       config,
		db:           db,
		logger:       logger,
		sessionStore: NewSessionStore(config.SessionSecret, db),
		sseBroker:    NewSSEBroker(logger),
		icClient:     idleclans.New(),
	}

	// Initialize OpenAI client if configured
	if config.OpenAIAPIKey != "" {
		model := config.OpenAIModel
		if model == "" {
			model = "gpt-4o" // Default to GPT-4o for vision
		}
		s.openaiClient = openai.NewClient(config.OpenAIAPIKey, model)
		logger.Info("OpenAI client initialized", zap.String("model", model))

		// Load embedded key reference images
		s.keyReferenceImages = NewKeyReferenceImages(logger)
	}

	return s, nil
}

// Start starts both the public and admin HTTP servers
func (s *Server) Start(ctx context.Context) error {
	// Setup public server routes
	publicMux := http.NewServeMux()
	s.setupPublicRoutes(publicMux)

	s.publicServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.PublicPort),
		Handler:      publicMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Setup admin server routes
	adminMux := http.NewServeMux()
	s.setupAdminRoutes(adminMux)

	s.adminServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.AdminPort),
		Handler:      adminMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start public server
	go func() {
		s.logger.Info("Starting public web server", zap.Int("port", s.config.PublicPort))
		if err := s.publicServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Public server error", zap.Error(err))
		}
	}()

	// Start admin server
	go func() {
		s.logger.Info("Starting admin web server", zap.Int("port", s.config.AdminPort))
		if err := s.adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Admin server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop gracefully shuts down both servers
func (s *Server) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var err error
	if s.publicServer != nil {
		if e := s.publicServer.Shutdown(shutdownCtx); e != nil {
			s.logger.Error("Error shutting down public server", zap.Error(e))
			err = e
		}
	}

	if s.adminServer != nil {
		if e := s.adminServer.Shutdown(shutdownCtx); e != nil {
			s.logger.Error("Error shutting down admin server", zap.Error(e))
			err = e
		}
	}

	return err
}

func (s *Server) setupPublicRoutes(mux *http.ServeMux) {
	// Auth routes
	mux.HandleFunc("GET /api/auth/login", s.handleLogin)
	mux.HandleFunc("GET /api/auth/callback", s.handleCallback)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("POST /api/auth/register", s.withAuth(s.handleRegister))

	// Protected API routes
	mux.HandleFunc("GET /api/me", s.withAuth(s.handleGetMe))
	mux.HandleFunc("PUT /api/quests/{playerName}/{boss}", s.withAuth(s.handleUpdateQuest))
	mux.HandleFunc("PUT /api/keys/{playerName}/{keyType}", s.withAuth(s.handleUpdateKeys))
	mux.HandleFunc("POST /api/alts", s.withAuth(s.handleAddAlt))
	mux.HandleFunc("DELETE /api/alts/{playerName}", s.withAuth(s.handleRemoveAlt))

	// Clan view routes (authenticated)
	mux.HandleFunc("GET /api/clan/bosses", s.withAuth(s.handleGetClanBosses))
	mux.HandleFunc("GET /api/clan/keys", s.withAuth(s.handleGetClanKeys))
	mux.HandleFunc("GET /api/clan/players", s.withAuth(s.handleGetClanPlayers))
	mux.HandleFunc("POST /api/clan/plan", s.withAuth(s.handleGetClanPlan))
	mux.HandleFunc("POST /api/clan/plan/send", s.withAuth(s.handleSendPlanToDiscord))

	// Screenshot analysis routes (authenticated)
	mux.HandleFunc("POST /api/analyze/quests", s.withAuth(s.handleAnalyzeQuests))
	mux.HandleFunc("POST /api/analyze/keys", s.withAuth(s.handleAnalyzeKeys))

	// SSE endpoint for live updates
	mux.HandleFunc("GET /api/events", s.handleSSE)

	// Serve static files for SPA
	mux.HandleFunc("/", s.handleStaticFiles)
}

func (s *Server) setupAdminRoutes(mux *http.ServeMux) {
	// Admin API routes (no auth required - internal network only)
	mux.HandleFunc("GET /api/admin/check", s.handleAdminCheck)
	mux.HandleFunc("GET /api/players", s.handleAdminGetPlayers)
	mux.HandleFunc("GET /api/players/{discordId}", s.handleAdminGetPlayer)
	mux.HandleFunc("PUT /api/players/{discordId}/quests/{playerName}/{boss}", s.handleAdminUpdateQuest)
	mux.HandleFunc("PUT /api/players/{discordId}/keys/{playerName}/{keyType}", s.handleAdminUpdateKeys)
	mux.HandleFunc("POST /api/players/{discordId}/unregister", s.handleAdminUnregisterPlayer)
	mux.HandleFunc("DELETE /api/players/{discordId}", s.handleAdminDeletePlayer)

	// Admin screenshot analysis routes (no auth required - internal network only)
	mux.HandleFunc("POST /api/admin/analyze/quests", s.handleAdminAnalyzeQuests)
	mux.HandleFunc("POST /api/admin/analyze/keys", s.handleAdminAnalyzeKeys)

	// SSE endpoint for live updates
	mux.HandleFunc("GET /api/events", s.handleAdminSSE)

	// Serve static files for admin SPA
	mux.HandleFunc("/", s.handleAdminStaticFiles)
}

