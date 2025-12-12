package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jirwin/idleclans/pkg/idleclans"
	"github.com/jirwin/idleclans/pkg/market"
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
	EnableMarket        bool   // Enable market price tracking
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
	config             *Config
	db                 *quests.DB
	logger             *zap.Logger
	publicServer       *http.Server
	adminServer        *http.Server
	publicMux          *http.ServeMux
	sessionStore       *SessionStore
	sseBroker          *SSEBroker
	icClient           *idleclans.Client
	discordSender      DiscordMessageSender
	openaiClient       *openai.Client
	keyReferenceImages *KeyReferenceImages
	// Market components
	marketDB        *market.DB
	marketCollector *market.Collector
	marketAPI       *MarketAPI
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

// InitMarket initializes the market tracking components
// This should be called after NewServer and Start if market tracking is enabled
func (s *Server) InitMarket(ctx context.Context) error {
	if !s.config.EnableMarket {
		return nil
	}

	// Create market DB using the same connection
	var err error
	s.marketDB, err = market.NewDB(s.db.GetDB(), s.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize market database: %w", err)
	}

	// Create collector
	s.marketCollector = market.NewCollector(s.marketDB, s.logger, nil)

	// Create API handler
	s.marketAPI = NewMarketAPI(s.marketDB, s.marketCollector, s.logger)

	// Register market routes now that the API is initialized
	// This is done after Start() so the server can respond to health checks immediately
	if s.publicMux != nil {
		s.marketAPI.RegisterRoutes(s.publicMux, "/api/market")
		s.logger.Info("Market API routes registered")
	}

	s.logger.Info("Market tracking initialized")
	return nil
}

// StartMarketCollector starts the background price collector
func (s *Server) StartMarketCollector(ctx context.Context) {
	if s.marketCollector != nil {
		s.marketCollector.Start(ctx)
	}
}

// StopMarketCollector stops the background price collector
func (s *Server) StopMarketCollector() {
	if s.marketCollector != nil {
		s.marketCollector.Stop()
	}
}

// Start starts both the public and admin HTTP servers
func (s *Server) Start(ctx context.Context) error {
	// Setup public server routes
	s.publicMux = http.NewServeMux()
	s.setupPublicRoutes(s.publicMux)

	s.publicServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.PublicPort),
		Handler:      s.publicMux,
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
	// Health check endpoint - responds immediately, useful for nginx/load balancers
	mux.HandleFunc("GET /health", s.handleHealthCheck)
	mux.HandleFunc("GET /api/health", s.handleHealthCheck)

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

	// Party routes (authenticated)
	mux.HandleFunc("POST /api/parties", s.withAuth(s.handleCreateParty))
	mux.HandleFunc("GET /api/parties", s.withAuth(s.handleGetUserParties))
	mux.HandleFunc("GET /api/parties/{partyId}", s.withAuth(s.handleGetParty))
	mux.HandleFunc("POST /api/parties/{partyId}/start", s.withAuth(s.handleStartPartyStep))
	mux.HandleFunc("PUT /api/parties/{partyId}/kills", s.withAuth(s.handleUpdatePartyKills))
	mux.HandleFunc("PUT /api/parties/{partyId}/keys", s.withAuth(s.handleUpdatePartyKeys))
	mux.HandleFunc("POST /api/parties/{partyId}/next-step", s.withAuth(s.handleNextPartyStep))
	mux.HandleFunc("POST /api/parties/{partyId}/end", s.withAuth(s.handleEndParty))

	// SSE endpoint for live updates
	mux.HandleFunc("GET /api/events", s.handleSSE)

	// Note: Market routes are registered dynamically after InitMarket() is called
	// This allows the server to start quickly and respond to health checks
	// before market initialization completes

	// Serve static files for SPA
	mux.HandleFunc("/", s.handleStaticFiles)
}

func (s *Server) setupAdminRoutes(mux *http.ServeMux) {
	// Health check for admin port too
	mux.HandleFunc("GET /health", s.handleHealthCheck)

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

// handleHealthCheck provides a simple health check endpoint for load balancers/nginx
func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
