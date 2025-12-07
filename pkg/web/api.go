package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jirwin/idleclans/pkg/quests"
	"go.uber.org/zap"
)

// UserData represents the full data for a user
type UserData struct {
	DiscordID  string                       `json:"discord_id"`
	Username   string                       `json:"username"`
	Avatar     string                       `json:"avatar"`
	PlayerName string                       `json:"player_name"`
	Alts       []string                     `json:"alts"`
	Quests     map[string][]Quest           `json:"quests"` // keyed by player name
	Keys       map[string]map[string]int    `json:"keys"`   // keyed by player name, then key type
}

// Quest represents a quest for the API
type Quest struct {
	BossName      string `json:"boss_name"`
	RequiredKills int    `json:"required_kills"`
	CurrentKills  int    `json:"current_kills"`
}

// PlayerData represents data for a player in admin view
type PlayerData struct {
	DiscordID  string         `json:"discord_id"`
	PlayerName string         `json:"player_name"`
	IsAlt      bool           `json:"is_alt"`
	OwnerName  string         `json:"owner_name,omitempty"` // Main character name if this is an alt
	Alts       []string       `json:"alts"`
	Quests     []Quest        `json:"quests"`
	Keys       map[string]int `json:"keys"`
}

// getWeekAndYear returns the current ISO week number and year
func getWeekAndYear() (int, int) {
	now := time.Now()
	year, week := now.ISOWeek()
	return week, year
}

// handleGetMe returns the authenticated user's data
func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	discordID := session.UserID

	// Get player name
	playerName, err := s.db.GetPlayerName(ctx, discordID)
	if err != nil {
		s.logger.Debug("No player registered", zap.String("discord_id", discordID))
		playerName = ""
	}

	// Get alts - ensure not nil
	alts, err := s.db.GetAlts(ctx, discordID)
	if err != nil {
		s.logger.Error("Failed to get alts", zap.Error(err))
	}
	if alts == nil {
		alts = []string{}
	}

	// Get quests for all player names
	week, year := getWeekAndYear()
	questsMap := make(map[string][]Quest)

	allNames := []string{}
	if playerName != "" {
		allNames = append(allNames, playerName)
	}
	allNames = append(allNames, alts...)

	// Maps keyed by player name
	keysMap := make(map[string]map[string]int)

	for _, name := range allNames {
		// Get quests for this player
		playerQuests, err := s.db.GetPlayerQuests(ctx, name, week, year)
		if err != nil {
			s.logger.Error("Failed to get quests", zap.Error(err), zap.String("player", name))
			continue
		}
		apiQuests := make([]Quest, len(playerQuests))
		for i, q := range playerQuests {
			apiQuests[i] = Quest{
				BossName:      q.BossName,
				RequiredKills: q.RequiredKills,
				CurrentKills:  q.CurrentKills,
			}
		}
		questsMap[name] = apiQuests

		// Get keys for this player
		playerKeys, err := s.db.GetPlayerKeys(ctx, name)
		if err != nil {
			s.logger.Error("Failed to get keys", zap.Error(err), zap.String("player", name))
		}
		if playerKeys == nil {
			playerKeys = make(map[string]int)
		}
		keysMap[name] = playerKeys
	}

	userData := UserData{
		DiscordID:  discordID,
		Username:   session.Username,
		Avatar:     session.Avatar,
		PlayerName: playerName,
		Alts:       alts,
		Quests:     questsMap,
		Keys:       keysMap,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userData)
}

// UpdateQuestRequest represents a request to update a quest
type UpdateQuestRequest struct {
	RequiredKills int `json:"required_kills"`
}

// handleUpdateQuest updates a quest for the authenticated user's character
func (s *Server) handleUpdateQuest(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	playerName := r.PathValue("playerName")
	boss := r.PathValue("boss")

	if playerName == "" || boss == "" {
		http.Error(w, "Missing player name or boss", http.StatusBadRequest)
		return
	}

	// Verify the player belongs to this user
	ctx := r.Context()
	if !s.userOwnsPlayer(ctx, session.UserID, playerName) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Validate boss name
	if !quests.IsValidBoss(boss) {
		http.Error(w, "Invalid boss name", http.StatusBadRequest)
		return
	}

	var req UpdateQuestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.RequiredKills < 0 {
		http.Error(w, "Required kills cannot be negative", http.StatusBadRequest)
		return
	}

	week, year := getWeekAndYear()
	err := s.db.UpsertQuest(ctx, session.UserID, playerName, week, year, boss, req.RequiredKills)
	if err != nil {
		s.logger.Error("Failed to update quest", zap.Error(err))
		http.Error(w, "Failed to update quest", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Quest updated",
		zap.String("user_id", session.UserID),
		zap.String("player", playerName),
		zap.String("boss", boss),
		zap.Int("kills", req.RequiredKills),
	)

	// Notify connected clients of the update
	s.NotifyDataChange("quest")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// UpdateKeysRequest represents a request to update keys
type UpdateKeysRequest struct {
	Count int `json:"count"`
}

// handleUpdateKeys updates keys for the authenticated user's character
func (s *Server) handleUpdateKeys(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	playerName := r.PathValue("playerName")
	keyType := r.PathValue("keyType")
	if playerName == "" || keyType == "" {
		http.Error(w, "Missing player name or key type", http.StatusBadRequest)
		return
	}

	// Verify the player belongs to this user
	ctx := r.Context()
	if !s.userOwnsPlayer(ctx, session.UserID, playerName) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Resolve key type
	resolvedKey, ok := quests.ResolveKeyType(keyType)
	if !ok {
		http.Error(w, "Invalid key type", http.StatusBadRequest)
		return
	}

	var req UpdateKeysRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Count < 0 {
		http.Error(w, "Count cannot be negative", http.StatusBadRequest)
		return
	}

	err := s.db.UpsertPlayerKeys(ctx, playerName, resolvedKey, req.Count)
	if err != nil {
		s.logger.Error("Failed to update keys", zap.Error(err))
		http.Error(w, "Failed to update keys", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Keys updated",
		zap.String("user_id", session.UserID),
		zap.String("player_name", playerName),
		zap.String("key_type", resolvedKey),
		zap.Int("count", req.Count),
	)

	// Notify connected clients of the update
	s.NotifyDataChange("keys")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// userOwnsPlayer checks if a Discord user owns a player (main or alt)
func (s *Server) userOwnsPlayer(ctx context.Context, discordID, playerName string) bool {
	// Check main player
	mainName, err := s.db.GetPlayerName(ctx, discordID)
	if err == nil && mainName == playerName {
		return true
	}

	// Check alts
	alts, err := s.db.GetAlts(ctx, discordID)
	if err != nil {
		return false
	}

	for _, alt := range alts {
		if alt == playerName {
			return true
		}
	}

	return false
}

// Admin API handlers

// handleAdminCheck returns confirmation that this is the admin server
func (s *Server) handleAdminCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"admin": true})
}

// handleAdminGetPlayers returns all players (including alts as separate entries)
func (s *Server) handleAdminGetPlayers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get all main players from the database
	rows, err := s.db.GetAllPlayers(ctx)
	if err != nil {
		s.logger.Error("Failed to get players", zap.Error(err))
		http.Error(w, "Failed to get players", http.StatusInternalServerError)
		return
	}

	// Initialize to empty slice (not nil) so JSON encodes as [] not null
	players := make([]PlayerData, 0)
	week, year := getWeekAndYear()

	for _, row := range rows {
		// Get alts - ensure not nil
		alts, _ := s.db.GetAlts(ctx, row.DiscordUserID)
		if alts == nil {
			alts = []string{}
		}

		// Get keys for main player - ensure not nil
		mainKeys, _ := s.db.GetPlayerKeys(ctx, row.PlayerName)
		if mainKeys == nil {
			mainKeys = make(map[string]int)
		}

		// Add main player
		mainQuests, _ := s.db.GetPlayerQuests(ctx, row.PlayerName, week, year)
		apiQuests := make([]Quest, 0, len(mainQuests))
		for _, q := range mainQuests {
			apiQuests = append(apiQuests, Quest{
				BossName:      q.BossName,
				RequiredKills: q.RequiredKills,
				CurrentKills:  q.CurrentKills,
			})
		}

		players = append(players, PlayerData{
			DiscordID:  row.DiscordUserID,
			PlayerName: row.PlayerName,
			IsAlt:      false,
			Alts:       alts,
			Quests:     apiQuests,
			Keys:       mainKeys,
		})

		// Add each alt as a separate entry
		for _, altName := range alts {
			// Get keys for this alt - ensure not nil
			altKeys, _ := s.db.GetPlayerKeys(ctx, altName)
			if altKeys == nil {
				altKeys = make(map[string]int)
			}

			altQuests, _ := s.db.GetPlayerQuests(ctx, altName, week, year)
			altApiQuests := make([]Quest, 0, len(altQuests))
			for _, q := range altQuests {
				altApiQuests = append(altApiQuests, Quest{
					BossName:      q.BossName,
					RequiredKills: q.RequiredKills,
					CurrentKills:  q.CurrentKills,
				})
			}

			players = append(players, PlayerData{
				DiscordID:  row.DiscordUserID,
				PlayerName: altName,
				IsAlt:      true,
				OwnerName:  row.PlayerName,
				Alts:       []string{}, // Alts don't have their own alts
				Quests:     altApiQuests,
				Keys:       altKeys,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(players)
}

// handleAdminGetPlayer returns a specific player's data
func (s *Server) handleAdminGetPlayer(w http.ResponseWriter, r *http.Request) {
	discordID := r.PathValue("discordId")
	if discordID == "" {
		http.Error(w, "Missing discord ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get player name
	playerName, err := s.db.GetPlayerName(ctx, discordID)
	if err != nil {
		http.Error(w, "Player not found", http.StatusNotFound)
		return
	}

	// Get alts - ensure not nil
	alts, _ := s.db.GetAlts(ctx, discordID)
	if alts == nil {
		alts = []string{}
	}

	// Get quests
	week, year := getWeekAndYear()
	questRows, _ := s.db.GetPlayerQuests(ctx, playerName, week, year)
	apiQuests := make([]Quest, 0, len(questRows))
	for _, q := range questRows {
		apiQuests = append(apiQuests, Quest{
			BossName:      q.BossName,
			RequiredKills: q.RequiredKills,
			CurrentKills:  q.CurrentKills,
		})
	}

	// Get keys for this player - ensure not nil
	keys, _ := s.db.GetPlayerKeys(ctx, playerName)
	if keys == nil {
		keys = make(map[string]int)
	}

	player := PlayerData{
		DiscordID:  discordID,
		PlayerName: playerName,
		Alts:       alts,
		Quests:     apiQuests,
		Keys:       keys,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(player)
}

// handleAdminUpdateQuest updates a quest for any player (main or alt)
func (s *Server) handleAdminUpdateQuest(w http.ResponseWriter, r *http.Request) {
	discordID := r.PathValue("discordId")
	playerName := r.PathValue("playerName")
	boss := r.PathValue("boss")

	if discordID == "" || playerName == "" || boss == "" {
		http.Error(w, "Missing discord ID, player name, or boss", http.StatusBadRequest)
		return
	}

	// Validate boss name
	if !quests.IsValidBoss(boss) {
		http.Error(w, "Invalid boss name", http.StatusBadRequest)
		return
	}

	var req UpdateQuestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	week, year := getWeekAndYear()
	err := s.db.UpsertQuest(ctx, discordID, playerName, week, year, boss, req.RequiredKills)
	if err != nil {
		s.logger.Error("Failed to update quest", zap.Error(err))
		http.Error(w, "Failed to update quest", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Admin updated quest",
		zap.String("discord_id", discordID),
		zap.String("player", playerName),
		zap.String("boss", boss),
		zap.Int("kills", req.RequiredKills),
	)

	// Notify connected clients of the update
	s.NotifyDataChange("quest")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleAdminUpdateKeys updates keys for any player (by player name)
func (s *Server) handleAdminUpdateKeys(w http.ResponseWriter, r *http.Request) {
	playerName := r.PathValue("playerName")
	keyType := r.PathValue("keyType")

	if playerName == "" || keyType == "" {
		http.Error(w, "Missing player name or key type", http.StatusBadRequest)
		return
	}

	// Resolve key type
	resolvedKey, ok := quests.ResolveKeyType(keyType)
	if !ok {
		http.Error(w, "Invalid key type", http.StatusBadRequest)
		return
	}

	var req UpdateKeysRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err := s.db.UpsertPlayerKeys(ctx, playerName, resolvedKey, req.Count)
	if err != nil {
		s.logger.Error("Failed to update keys", zap.Error(err))
		http.Error(w, "Failed to update keys", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Admin updated keys",
		zap.String("player_name", playerName),
		zap.String("key_type", resolvedKey),
		zap.Int("count", req.Count),
	)

	// Notify connected clients of the update
	s.NotifyDataChange("keys")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// Helper to parse int from string with default
func parseInt(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

