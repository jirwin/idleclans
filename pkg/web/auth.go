package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jirwin/idleclans/pkg/quests"
	"go.uber.org/zap"
)

const (
	discordAuthorizeURL = "https://discord.com/api/oauth2/authorize"
	discordTokenURL     = "https://discord.com/api/oauth2/token"
	discordUserURL      = "https://discord.com/api/users/@me"
	sessionCookieName   = "session"
	stateCookieName     = "oauth_state"
)

// DiscordUser represents a Discord user from the API
type DiscordUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	Avatar        string `json:"avatar"`
	GlobalName    string `json:"global_name"`
}

// Session represents a user session
type Session struct {
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Avatar    string    `json:"avatar"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SessionStore manages user sessions with database persistence
type SessionStore struct {
	secret string
	db     *quests.DB
}

// NewSessionStore creates a new session store
func NewSessionStore(secret string, db *quests.DB) *SessionStore {
	return &SessionStore{
		secret: secret,
		db:     db,
	}
}

// Create creates a new session for a user
func (s *SessionStore) Create(user *DiscordUser) (string, error) {
	// Generate random session ID
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	sessionID := hex.EncodeToString(b)

	// Create session with 7 day expiry
	expiresAt := time.Now().Add(24 * time.Hour * 7)

	ctx := context.Background()
	err := s.db.CreateSession(ctx, sessionID, user.ID, user.Username, user.Avatar, expiresAt)
	if err != nil {
		return "", err
	}

	// Sign the session ID
	return s.sign(sessionID), nil
}

// Get retrieves a session by signed session ID
func (s *SessionStore) Get(signedID string) (*Session, bool) {
	sessionID, valid := s.verify(signedID)
	if !valid {
		return nil, false
	}

	ctx := context.Background()
	dbSession, err := s.db.GetSession(ctx, sessionID)
	if err != nil || dbSession == nil {
		return nil, false
	}

	if time.Now().After(dbSession.ExpiresAt) {
		s.Delete(signedID)
		return nil, false
	}

	return &Session{
		UserID:    dbSession.UserID,
		Username:  dbSession.Username,
		Avatar:    dbSession.Avatar,
		ExpiresAt: dbSession.ExpiresAt,
	}, true
}

// Delete removes a session
func (s *SessionStore) Delete(signedID string) {
	sessionID, valid := s.verify(signedID)
	if !valid {
		return
	}

	ctx := context.Background()
	s.db.DeleteSession(ctx, sessionID)
}

func (s *SessionStore) sign(data string) string {
	h := hmac.New(sha256.New, []byte(s.secret))
	h.Write([]byte(data))
	signature := base64.URLEncoding.EncodeToString(h.Sum(nil))
	return data + "." + signature
}

func (s *SessionStore) verify(signed string) (string, bool) {
	parts := strings.SplitN(signed, ".", 2)
	if len(parts) != 2 {
		return "", false
	}

	data, signature := parts[0], parts[1]
	h := hmac.New(sha256.New, []byte(s.secret))
	h.Write([]byte(data))
	expectedSig := base64.URLEncoding.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return "", false
	}

	return data, true
}

// handleLogin redirects to Discord OAuth
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Generate state for CSRF protection
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		s.logger.Error("Failed to generate state", zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	state := hex.EncodeToString(stateBytes)

	// Store state in cookie
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(s.config.BaseURL, "https"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600, // 10 minutes
	})

	// Build Discord OAuth URL
	params := url.Values{
		"client_id":     {s.config.DiscordClientID},
		"redirect_uri":  {s.config.BaseURL + "/api/auth/callback"},
		"response_type": {"code"},
		"scope":         {"identify"},
		"state":         {state},
	}

	http.Redirect(w, r, discordAuthorizeURL+"?"+params.Encode(), http.StatusTemporaryRedirect)
}

// handleCallback handles the OAuth callback from Discord
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Verify state
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil {
		s.logger.Warn("No state cookie found")
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	state := r.URL.Query().Get("state")
	if state != stateCookie.Value {
		s.logger.Warn("State mismatch", zap.String("expected", stateCookie.Value), zap.String("got", state))
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	// Check for error from Discord
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		s.logger.Warn("Discord OAuth error", zap.String("error", errParam))
		http.Redirect(w, r, "/?error=auth_failed", http.StatusTemporaryRedirect)
		return
	}

	// Exchange code for token
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "No code provided", http.StatusBadRequest)
		return
	}

	token, err := s.exchangeCode(code)
	if err != nil {
		s.logger.Error("Failed to exchange code", zap.Error(err))
		http.Redirect(w, r, "/?error=auth_failed", http.StatusTemporaryRedirect)
		return
	}

	// Get user info
	user, err := s.getDiscordUser(token)
	if err != nil {
		s.logger.Error("Failed to get user info", zap.Error(err))
		http.Redirect(w, r, "/?error=auth_failed", http.StatusTemporaryRedirect)
		return
	}

	// Create session
	sessionID, err := s.sessionStore.Create(user)
	if err != nil {
		s.logger.Error("Failed to create session", zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(s.config.BaseURL, "https"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 7, // 7 days
	})

	s.logger.Info("User logged in", zap.String("user_id", user.ID), zap.String("username", user.Username))

	// Redirect to dashboard
	http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
}

// handleLogout clears the session
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		s.sessionStore.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "logged out"})
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (s *Server) exchangeCode(code string) (string, error) {
	data := url.Values{
		"client_id":     {s.config.DiscordClientID},
		"client_secret": {s.config.DiscordClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {s.config.BaseURL + "/api/auth/callback"},
	}

	resp, err := http.PostForm(discordTokenURL, data)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request returned status %d", resp.StatusCode)
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	return tokenResp.AccessToken, nil
}

func (s *Server) getDiscordUser(accessToken string) (*DiscordUser, error) {
	req, err := http.NewRequest("GET", discordUserURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user request returned status %d", resp.StatusCode)
	}

	var user DiscordUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	return &user, nil
}

// RegisterRequest represents a request to register a character
type RegisterRequest struct {
	PlayerName string `json:"player_name"`
}

// RegisterResponse represents the response from registration
type RegisterResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// handleRegister handles character registration for new users
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	// Check if user already has a character registered
	existingPlayer, err := s.db.GetPlayerName(ctx, session.UserID)
	if err == nil && existingPlayer != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(RegisterResponse{
			Success: false,
			Error:   "You already have a character registered: " + existingPlayer,
		})
		return
	}

	// Parse request
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(RegisterResponse{
			Success: false,
			Error:   "Invalid request body",
		})
		return
	}

	if req.PlayerName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(RegisterResponse{
			Success: false,
			Error:   "Player name is required",
		})
		return
	}

	// Fetch player from IdleClans API
	s.logger.Info("Looking up player in IdleClans API",
		zap.String("player_name", req.PlayerName))
	
	player, err := s.icClient.GetPlayer(ctx, req.PlayerName)
	if err != nil {
		s.logger.Warn("Failed to fetch player from IdleClans API",
			zap.String("player_name", req.PlayerName),
			zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(RegisterResponse{
			Success: false,
			Error:   "Could not find player '" + req.PlayerName + "' in IdleClans. Please check the spelling.",
		})
		return
	}

	// Check if player data is valid (API might return empty player for non-existent names)
	if player == nil || player.Username == "" {
		s.logger.Warn("IdleClans API returned empty player",
			zap.String("player_name", req.PlayerName))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(RegisterResponse{
			Success: false,
			Error:   "Could not find player '" + req.PlayerName + "' in IdleClans. Please check the spelling.",
		})
		return
	}

	s.logger.Info("Found player in IdleClans API",
		zap.String("player_name", player.Username),
		zap.String("player_guild", player.GuildName),
		zap.String("required_guild", s.config.RequiredGuild))

	// Check guild requirement if configured
	if s.config.RequiredGuild != "" {
		if !strings.EqualFold(player.GuildName, s.config.RequiredGuild) {
			s.logger.Warn("Player guild does not match required guild",
				zap.String("player_name", req.PlayerName),
				zap.String("player_guild", player.GuildName),
				zap.String("required_guild", s.config.RequiredGuild))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(RegisterResponse{
				Success: false,
				Error:   fmt.Sprintf("This app is only for members of '%s'. Your character '%s' is in guild '%s'.",
					s.config.RequiredGuild, player.Username, player.GuildName),
			})
			return
		}
	}

	// Register the player
	err = s.db.RegisterPlayer(ctx, session.UserID, player.Username)
	if err != nil {
		s.logger.Error("Failed to register player",
			zap.String("discord_id", session.UserID),
			zap.String("player_name", player.Username),
			zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(RegisterResponse{
			Success: false,
			Error:   "Failed to register character. Please try again.",
		})
		return
	}

	s.logger.Info("Player registered successfully",
		zap.String("discord_id", session.UserID),
		zap.String("discord_username", session.Username),
		zap.String("player_name", player.Username),
		zap.String("guild", player.GuildName))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(RegisterResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully registered character '%s'!", player.Username),
	})
}

