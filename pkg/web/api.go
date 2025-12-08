package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "image/gif"
	_ "image/jpeg"

	"github.com/jirwin/idleclans/pkg/openai"
	"github.com/jirwin/idleclans/pkg/quests"
	"go.uber.org/zap"
)

// UserData represents the full data for a user
type UserData struct {
	DiscordID  string                    `json:"discord_id"`
	Username   string                    `json:"username"`
	Avatar     string                    `json:"avatar"`
	PlayerName string                    `json:"player_name"`
	Alts       []string                  `json:"alts"`
	Quests     map[string][]Quest        `json:"quests"` // keyed by player name
	Keys       map[string]map[string]int `json:"keys"`   // keyed by player name, then key type
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

// getWeekAndYear returns the current ISO week number and year (based on UTC)
func getWeekAndYear() (int, int) {
	now := time.Now().UTC()
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

// Alt management handlers

// AddAltRequest represents a request to add an alt character
type AddAltRequest struct {
	PlayerName string `json:"player_name"`
}

// handleAddAlt adds an alt character for the authenticated user
func (s *Server) handleAddAlt(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	// Check if user has a main character registered
	mainPlayer, err := s.db.GetPlayerName(ctx, session.UserID)
	if err != nil || mainPlayer == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "You must register a main character before adding alts",
		})
		return
	}

	// Parse request
	var req AddAltRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	if req.PlayerName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Player name is required"})
		return
	}

	// Check if it's not the same as main
	if req.PlayerName == mainPlayer {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Cannot add your main character as an alt"})
		return
	}

	// Check if already an alt
	alts, _ := s.db.GetAlts(ctx, session.UserID)
	for _, alt := range alts {
		if alt == req.PlayerName {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "This character is already registered as an alt"})
			return
		}
	}

	// Fetch player from IdleClans API to verify
	s.logger.Info("Looking up alt in IdleClans API",
		zap.String("player_name", req.PlayerName))

	player, err := s.icClient.GetPlayer(ctx, req.PlayerName)
	if err != nil {
		s.logger.Warn("Failed to fetch player from IdleClans API",
			zap.String("player_name", req.PlayerName),
			zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Could not find player '" + req.PlayerName + "' in IdleClans. Please check the spelling.",
		})
		return
	}

	// Check if player data is valid
	if player == nil || player.Username == "" {
		s.logger.Warn("IdleClans API returned empty player for alt",
			zap.String("player_name", req.PlayerName))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Could not find player '" + req.PlayerName + "' in IdleClans. Please check the spelling.",
		})
		return
	}

	s.logger.Info("Found alt in IdleClans API",
		zap.String("player_name", player.Username),
		zap.String("player_guild", player.GuildName),
		zap.String("required_guild", s.config.RequiredGuild))

	// Check guild requirement if configured
	if s.config.RequiredGuild != "" {
		if !strings.EqualFold(player.GuildName, s.config.RequiredGuild) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("Alt must be in '%s'. Character '%s' is in guild '%s'.",
					s.config.RequiredGuild, player.Username, player.GuildName),
			})
			return
		}
	}

	// Register the alt
	err = s.db.RegisterAlt(ctx, session.UserID, player.Username)
	if err != nil {
		s.logger.Error("Failed to register alt",
			zap.String("discord_id", session.UserID),
			zap.String("alt_name", player.Username),
			zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to register alt. Please try again."})
		return
	}

	s.logger.Info("Alt registered successfully",
		zap.String("discord_id", session.UserID),
		zap.String("main_player", mainPlayer),
		zap.String("alt_name", player.Username))

	// Notify connected clients
	s.NotifyDataChange("alt")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": fmt.Sprintf("Successfully added alt '%s'!", player.Username),
	})
}

// handleRemoveAlt removes an alt character for the authenticated user
func (s *Server) handleRemoveAlt(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	playerName := r.PathValue("playerName")
	if playerName == "" {
		http.Error(w, "Player name is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Verify this is actually one of the user's alts
	alts, _ := s.db.GetAlts(ctx, session.UserID)
	isAlt := false
	for _, alt := range alts {
		if alt == playerName {
			isAlt = true
			break
		}
	}

	if !isAlt {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "This character is not one of your alts"})
		return
	}

	// Remove the alt
	err := s.db.RemoveAlt(ctx, session.UserID, playerName)
	if err != nil {
		s.logger.Error("Failed to remove alt",
			zap.String("discord_id", session.UserID),
			zap.String("alt_name", playerName),
			zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to remove alt. Please try again."})
		return
	}

	s.logger.Info("Alt removed successfully",
		zap.String("discord_id", session.UserID),
		zap.String("alt_name", playerName))

	// Notify connected clients
	s.NotifyDataChange("alt")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": fmt.Sprintf("Removed alt '%s'", playerName),
	})
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

// handleAdminUnregisterPlayer removes the player registration (keeps quests/keys data)
func (s *Server) handleAdminUnregisterPlayer(w http.ResponseWriter, r *http.Request) {
	discordID := r.PathValue("discordId")
	if discordID == "" {
		http.Error(w, "Missing discord ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err := s.db.UnregisterPlayer(ctx, discordID)
	if err != nil {
		s.logger.Error("Failed to unregister player", zap.Error(err))
		http.Error(w, "Failed to unregister player", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Admin unregistered player", zap.String("discord_id", discordID))

	// Notify connected clients
	s.NotifyDataChange("player")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "unregistered"})
}

// handleAdminDeletePlayer removes a player and all their data
func (s *Server) handleAdminDeletePlayer(w http.ResponseWriter, r *http.Request) {
	discordID := r.PathValue("discordId")
	if discordID == "" {
		http.Error(w, "Missing discord ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err := s.db.DeletePlayer(ctx, discordID)
	if err != nil {
		s.logger.Error("Failed to delete player", zap.Error(err))
		http.Error(w, "Failed to delete player", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Admin deleted player", zap.String("discord_id", discordID))

	// Notify connected clients
	s.NotifyDataChange("player")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
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

// Clan view data structures

// ClanBossEntry represents a player's remaining kills for a boss
type ClanBossEntry struct {
	PlayerName     string `json:"player_name"`
	RemainingKills int    `json:"remaining_kills"`
}

// ClanBossData represents all players grouped by boss
type ClanBossData struct {
	Week   int                        `json:"week"`
	Year   int                        `json:"year"`
	Bosses map[string][]ClanBossEntry `json:"bosses"`
}

// ClanKeyEntry represents a player's key count
type ClanKeyEntry struct {
	PlayerName string `json:"player_name"`
	Count      int    `json:"count"`
}

// ClanKeysData represents all player keys grouped by key type
type ClanKeysData struct {
	Keys map[string][]ClanKeyEntry `json:"keys"`
}

// handleGetClanBosses returns all quests grouped by boss for the current week
func (s *Server) handleGetClanBosses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	week, year := getWeekAndYear()

	questsList, err := s.db.GetAllQuestsForWeek(ctx, week, year)
	if err != nil {
		s.logger.Error("Failed to get quests", zap.Error(err))
		http.Error(w, "Failed to get quests", http.StatusInternalServerError)
		return
	}

	// Group by boss
	bosses := make(map[string][]ClanBossEntry)
	for _, quest := range questsList {
		remainingKills := quest.RequiredKills - quest.CurrentKills
		if remainingKills <= 0 {
			continue
		}

		bosses[quest.BossName] = append(bosses[quest.BossName], ClanBossEntry{
			PlayerName:     quest.PlayerName,
			RemainingKills: remainingKills,
		})
	}

	// Ensure we return empty object not null
	if bosses == nil {
		bosses = make(map[string][]ClanBossEntry)
	}

	data := ClanBossData{
		Week:   week,
		Year:   year,
		Bosses: bosses,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// handleGetClanKeys returns all player keys grouped by key type
func (s *Server) handleGetClanKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	allKeys, err := s.db.GetAllPlayerKeys(ctx)
	if err != nil {
		s.logger.Error("Failed to get all player keys", zap.Error(err))
		http.Error(w, "Failed to get keys", http.StatusInternalServerError)
		return
	}

	// Group by key type
	keys := make(map[string][]ClanKeyEntry)
	for _, k := range allKeys {
		if k.Count <= 0 {
			continue
		}
		keys[k.KeyType] = append(keys[k.KeyType], ClanKeyEntry{
			PlayerName: k.PlayerName,
			Count:      k.Count,
		})
	}

	// Ensure we return empty object not null
	if keys == nil {
		keys = make(map[string][]ClanKeyEntry)
	}

	data := ClanKeysData{
		Keys: keys,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// handleGetClanPlayers returns all registered player names (main + alts)
func (s *Server) handleGetClanPlayers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	players, err := s.db.GetAllRegisteredPlayerNames(ctx)
	if err != nil {
		s.logger.Error("Failed to get all player names", zap.Error(err))
		http.Error(w, "Failed to get players", http.StatusInternalServerError)
		return
	}

	if players == nil {
		players = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]string{"players": players})
}

// Plan API types

// PlanRequest represents a request to generate a party plan
type PlanRequest struct {
	OnlinePlayers []string `json:"online_players"` // Optional filter for online players
}

// PlanPartyTask represents a task in a party
type PlanPartyTask struct {
	BossName  string `json:"boss_name"`
	Kills     int    `json:"kills"`
	KeyHolder string `json:"key_holder,omitempty"`
	KeyType   string `json:"key_type"`
	NoKeys    bool   `json:"no_keys"`
}

// PlanParty represents a party in the plan
type PlanParty struct {
	Players []string        `json:"players"`
	Tasks   []PlanPartyTask `json:"tasks"`
}

// PlanLeftover represents a player with unmet needs
type PlanLeftover struct {
	PlayerName string         `json:"player_name"`
	Needs      map[string]int `json:"needs"` // boss -> kills needed
}

// PlanData represents the full plan response
type PlanData struct {
	Week      int            `json:"week"`
	Year      int            `json:"year"`
	Parties   []PlanParty    `json:"parties"`
	Leftovers []PlanLeftover `json:"leftovers"`
}

// handleGetClanPlan generates a party plan for the current week
func (s *Server) handleGetClanPlan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	week, year := getWeekAndYear()

	// Parse request body for optional online players filter
	var req PlanRequest
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req) // Ignore errors, use defaults
	}

	planner := quests.NewPlanner(s.db)
	plan, err := planner.GeneratePlanFiltered(ctx, week, year, req.OnlinePlayers)
	if err != nil {
		s.logger.Error("Failed to generate plan", zap.Error(err))
		http.Error(w, "Failed to generate plan", http.StatusInternalServerError)
		return
	}

	// Convert to API types
	parties := make([]PlanParty, 0, len(plan.Parties))
	for _, p := range plan.Parties {
		tasks := make([]PlanPartyTask, 0, len(p.Tasks))
		for _, t := range p.Tasks {
			tasks = append(tasks, PlanPartyTask{
				BossName:  t.BossName,
				Kills:     t.Kills,
				KeyHolder: t.KeyHolder,
				KeyType:   t.KeyType,
				NoKeys:    t.NoKeys,
			})
		}
		parties = append(parties, PlanParty{
			Players: p.Players,
			Tasks:   tasks,
		})
	}

	leftovers := make([]PlanLeftover, 0, len(plan.Leftovers))
	for _, l := range plan.Leftovers {
		// Only include players with actual remaining needs
		needs := make(map[string]int)
		for boss, n := range l.Needs {
			if n > 0 {
				needs[boss] = n
			}
		}
		if len(needs) > 0 {
			leftovers = append(leftovers, PlanLeftover{
				PlayerName: l.Name,
				Needs:      needs,
			})
		}
	}

	data := PlanData{
		Week:      week,
		Year:      year,
		Parties:   parties,
		Leftovers: leftovers,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// SendPlanRequest represents a request to send a plan to Discord
type SendPlanRequest struct {
	Players []string `json:"players"` // Player names to include in the plan
	NoPing  bool     `json:"no_ping"` // If true, don't ping users
}

// handleSendPlanToDiscord sends a plan message to Discord as an embed
func (s *Server) handleSendPlanToDiscord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.discordSender == nil {
		http.Error(w, "Discord integration not configured", http.StatusServiceUnavailable)
		return
	}

	if s.config.DiscordChannelID == "" {
		http.Error(w, "Discord channel not configured", http.StatusServiceUnavailable)
		return
	}

	var req SendPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Players) == 0 {
		http.Error(w, "No players specified", http.StatusBadRequest)
		return
	}

	// Generate the plan
	week, year := getWeekAndYear()
	planner := quests.NewPlanner(s.db)
	plan, err := planner.GeneratePlanFiltered(ctx, week, year, req.Players)
	if err != nil {
		s.logger.Error("Failed to generate plan", zap.Error(err))
		http.Error(w, "Failed to generate plan", http.StatusInternalServerError)
		return
	}

	// Build ping string (unique users only) - skip if NoPing is set
	var pingContent string
	if !req.NoPing {
		playerDiscordIDs := make(map[string]string)
		for _, playerName := range req.Players {
			discordID, err := s.db.GetDiscordUserIDForPlayer(ctx, playerName)
			if err == nil && discordID != "" {
				playerDiscordIDs[playerName] = discordID
			}
		}

		seenIDs := make(map[string]bool)
		var pings []string
		for _, discordID := range playerDiscordIDs {
			if !seenIDs[discordID] {
				seenIDs[discordID] = true
				pings = append(pings, fmt.Sprintf("<@%s>", discordID))
			}
		}
		pingContent = strings.Join(pings, " ")
	}

	// Build embed fields - only tasks with keys
	var fields []DiscordEmbedField
	for _, party := range plan.Parties {
		// Filter tasks to only those with keys
		var tasksWithKeys []quests.PartyTask
		for _, task := range party.Tasks {
			if !task.NoKeys {
				tasksWithKeys = append(tasksWithKeys, task)
			}
		}

		if len(tasksWithKeys) == 0 {
			continue
		}

		// Build task list using player names (not mentions)
		var taskLines []string
		for _, task := range tasksWithKeys {
			bossLabel := strings.Title(task.BossName)
			taskLines = append(taskLines, fmt.Sprintf("• %s: %d (Key: %s)", bossLabel, task.Kills, task.KeyHolder))
		}

		fields = append(fields, DiscordEmbedField{
			Name:   strings.Join(party.Players, ", "),
			Value:  strings.Join(taskLines, "\n"),
			Inline: false,
		})
	}

	if len(fields) == 0 {
		http.Error(w, "No tasks with keys available to send", http.StatusBadRequest)
		return
	}

	embed := &DiscordEmbed{
		Title:  "Time to do quests!",
		Color:  0xe91e63, // Pink
		Fields: fields,
	}

	err = s.discordSender.SendMessageWithEmbed(s.config.DiscordChannelID, pingContent, embed)
	if err != nil {
		s.logger.Error("Failed to send Discord embed", zap.Error(err))
		http.Error(w, "Failed to send message to Discord", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Sent plan embed to Discord",
		zap.String("channel_id", s.config.DiscordChannelID),
		zap.Int("players", len(req.Players)),
		zap.Int("groups", len(fields)),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
}

// Screenshot analysis types and handlers

// AnalyzeQuestsResponse represents the response from quest screenshot analysis
type AnalyzeQuestsResponse struct {
	Bosses  []BossKillResult `json:"bosses"`
	Applied bool             `json:"applied"`
	Error   string           `json:"error,omitempty"`
}

// ImageSplitInfo contains info about how the image was split
type ImageSplitInfo struct {
	OriginalWidth  int
	OriginalHeight int
	TileSize       int
	TileCount      int
}

// splitImageIntoTiles splits a horizontal strip of key tiles into individual images
// It auto-detects tile boundaries by assuming square tiles based on image height
func splitImageIntoTiles(imgData []byte) ([][]byte, *ImageSplitInfo, error) {
	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode image: %w", err)
	}

	bounds := img.Bounds()
	height := bounds.Dy()
	width := bounds.Dx()

	// Calculate tile size - assume tiles are square
	// First try: number of tiles = width / height (rounded)
	numTiles := (width + height/2) / height
	if numTiles < 1 {
		numTiles = 1
	}

	// Calculate actual tile width
	tileSize := width / numTiles

	info := &ImageSplitInfo{
		OriginalWidth:  width,
		OriginalHeight: height,
		TileSize:       tileSize,
		TileCount:      numTiles,
	}

	var tiles [][]byte
	for i := 0; i < numTiles; i++ {
		x := bounds.Min.X + i*tileSize
		tileMaxX := x + tileSize
		if i == numTiles-1 {
			// Last tile gets any remaining pixels
			tileMaxX = bounds.Max.X
		}

		// Create subimage for this tile
		tileRect := image.Rect(x, bounds.Min.Y, tileMaxX, bounds.Max.Y)

		// Create a new image for the tile
		tileImg := image.NewRGBA(image.Rect(0, 0, tileRect.Dx(), tileRect.Dy()))
		for ty := 0; ty < tileRect.Dy(); ty++ {
			for tx := 0; tx < tileRect.Dx(); tx++ {
				tileImg.Set(tx, ty, img.At(tileRect.Min.X+tx, tileRect.Min.Y+ty))
			}
		}

		// Encode tile to PNG
		var buf bytes.Buffer
		if err := png.Encode(&buf, tileImg); err != nil {
			return nil, nil, fmt.Errorf("failed to encode tile: %w", err)
		}

		tiles = append(tiles, buf.Bytes())
	}

	return tiles, info, nil
}

// singleKeyResponse represents the LLM response for a single key tile
type singleKeyResponse struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// countResponse represents the LLM response with a count
type countResponse struct {
	Count int `json:"count"`
}

// splitImageIntoNTiles splits an image into exactly n equal-width tiles
func splitImageIntoNTiles(imgData []byte, n int) ([][]byte, error) {
	if n < 1 {
		n = 1
	}

	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	tileWidth := width / n

	var tiles [][]byte
	for i := 0; i < n; i++ {
		x := bounds.Min.X + i*tileWidth
		maxX := x + tileWidth
		if i == n-1 {
			// Last tile gets any remaining pixels
			maxX = bounds.Max.X
		}

		// Create tile image
		tileImg := image.NewRGBA(image.Rect(0, 0, maxX-x, height))
		for ty := 0; ty < height; ty++ {
			for tx := 0; tx < maxX-x; tx++ {
				tileImg.Set(tx, ty, img.At(x+tx, bounds.Min.Y+ty))
			}
		}

		// Encode to PNG
		var buf bytes.Buffer
		if err := png.Encode(&buf, tileImg); err != nil {
			return nil, fmt.Errorf("failed to encode tile: %w", err)
		}

		tiles = append(tiles, buf.Bytes())
	}

	return tiles, nil
}

// cropImageToBox extracts a portion of an image given bounding box coordinates
func cropImageToBox(imgData []byte, x, y, w, h int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	bounds := img.Bounds()

	// Validate and clamp coordinates
	if x < bounds.Min.X {
		x = bounds.Min.X
	}
	if y < bounds.Min.Y {
		y = bounds.Min.Y
	}
	if x+w > bounds.Max.X {
		w = bounds.Max.X - x
	}
	if y+h > bounds.Max.Y {
		h = bounds.Max.Y - y
	}

	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("invalid crop dimensions")
	}

	// Create cropped image
	cropped := image.NewRGBA(image.Rect(0, 0, w, h))
	for cy := 0; cy < h; cy++ {
		for cx := 0; cx < w; cx++ {
			cropped.Set(cx, cy, img.At(x+cx, y+cy))
		}
	}

	// Encode to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, cropped); err != nil {
		return nil, fmt.Errorf("failed to encode cropped image: %w", err)
	}

	return buf.Bytes(), nil
}

// BossKillResult represents a boss and its required kills from screenshot analysis
type BossKillResult struct {
	Name  string `json:"name"`
	Kills int    `json:"kills"`
}

// AnalyzeKeysResponse represents the response from key screenshot analysis
type AnalyzeKeysResponse struct {
	Keys    []KeyCountResult `json:"keys"`
	Applied bool             `json:"applied"`
	Error   string           `json:"error,omitempty"`
}

// KeyCountResult represents a key type and its count from screenshot analysis
type KeyCountResult struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// LLM response types for parsing
type llmQuestResponse struct {
	Bosses []struct {
		Name  string `json:"name"`
		Kills int    `json:"kills"`
	} `json:"bosses"`
}

type llmKeyResponse struct {
	Keys []struct {
		Type  string `json:"type"`
		Count int    `json:"count"`
	} `json:"keys"`
}

const questAnalysisPrompt = `You are analyzing a screenshot from the game IdleClans showing a quest tracker.

Each quest entry has this format:
- Quest title (e.g., "The godslayer")
- Boss target line: "Kill [BossName]" (e.g., "Kill Chimera", "Kill Zeus")
- Progress bar showing: "Completions: [current]/[required] | [percentage]%"

IMPORTANT: Extract the REQUIRED KILLS number - this is the number AFTER the slash.
For example: "Completions: 0/52 | 0%" means 52 required kills.

Valid boss names are: griffin, medusa, hades, zeus, devil, chimera, dragon, sobek, kronos

For each quest visible, extract:
1. The boss name from the "Kill [BossName]" line
2. The required kills (number after the slash in the completions line)

Return ONLY a JSON object:
{"bosses": [{"name": "chimera", "kills": 52}, {"name": "kronos", "kills": 6}]}

Rules:
- Use lowercase boss names exactly as listed above
- Only include quests with required kills > 0
- The "kills" value is the TOTAL required, not current progress`

// keyDescribePrompt asks the model to describe each key tile
const keyDescribePrompt = `Look at this game inventory showing boss keys in a row.

For EACH key tile from LEFT to RIGHT, describe:
1. The key's COLOR (brown, gold, red, blue, green, gray, white, cyan)
2. The key's SHAPE (simple house key OR ornate skeleton key)
3. The NUMBER shown in orange (or "none" if no number)

Format each key on its own line like this:
Key 1: [COLOR] [SHAPE], number: [NUMBER]
Key 2: [COLOR] [SHAPE], number: [NUMBER]
...

Be thorough - describe EVERY key tile you can see, from left to right.`

// keyAnalysisPrompt is the prompt without reference images
const keyAnalysisPrompt = `Analyze this inventory screenshot from IdleClans. Find ONLY boss keys and the Kronos book.

STRICT RULES:
1. ONLY include items that have a DISTINCT KEY SHAPE (handle + teeth/blade) or the Kronos book
2. The item MUST have a visible orange/yellow number displayed on it
3. IGNORE greyed-out or faded items - they are not valid
4. IGNORE ores, rocks, gems, tools, or any non-key items even if they have numbers
5. Stone keys are GRAY/SILVER colored keys - do NOT confuse with brown/tan ore rocks

KEY IDENTIFICATION (by color AND shape):
- Brown/tan KEY shape = "mountain"
- Gold/yellow KEY shape = "godly"
- Red KEY shape = "burning"
- Dark blue KEY shape = "underworld"
- Green KEY shape = "mutated"
- Gray/silver KEY shape = "stone" (NOT rocks or ore)
- White KEY shape = "ancient"
- Cyan/light blue KEY shape = "otherworldly"
- Red/brown BOOK with spots = "kronos"

IMPORTANT: Ore and rocks are NOT keys. If it doesn't have a clear key shape (handle + teeth), skip it.

Return JSON with only confirmed keys: {"keys":[{"type":"godly","count":39}]}`

// keyAnalysisPromptWithRefs is the prompt when reference images are provided
const keyAnalysisPromptWithRefs = `Analyze the inventory screenshot to find boss keys and the Kronos book.

I've provided reference images showing what each key type looks like. Use these to identify keys in the main image.

STRICT RULES:
1. Match items to the reference images - only include items that look like the reference keys
2. The item MUST have a visible orange/yellow number displayed on it
3. IGNORE greyed-out or faded items
4. IGNORE ores, rocks, gems, tools - they are NOT keys even if they have numbers
5. If an item doesn't match any reference image, skip it

Return JSON with only confirmed keys: {"keys":[{"type":"godly","count":39}]}`

// keyParsePrompt converts the description to JSON
const keyParsePrompt = `Convert this key description to JSON.

KEY TYPE MAPPING:
- brown/tan key = "mountain"
- gold/yellow key = "godly"
- red key = "burning"
- dark blue house key = "underworld"
- green key = "mutated"
- gray/silver key = "stone"
- white skeleton key = "ancient"
- cyan/light blue skeleton key = "otherworldly"

RULES:
1. Only include keys that have a number (skip "none")
2. Each key type can only appear once
3. Return ONLY valid JSON, no other text

OUTPUT FORMAT:
{"keys":[{"type":"godly","count":19},{"type":"stone","count":41}]}`

// singleKeyAnalysisPrompt is used when analyzing one key tile at a time
const singleKeyAnalysisPrompt = `Identify this game key icon.

KEY COLORS: mountain=BROWN, godly=GOLD, burning=RED, underworld=DARK_BLUE, mutated=GREEN, stone=GRAY, ancient=WHITE, otherworldly=CYAN

Look at the key color and the orange number (if any).

OUTPUT FORMAT - respond with ONLY this, no other text:
{"type":"X","count":N}

Replace X with key type, N with number (use 0 if no number shown).

VALID RESPONSES:
{"type":"godly","count":19}
{"type":"stone","count":41}
{"type":"burning","count":0}`

// countKeysPrompt asks the LLM to count visible key tiles
const countKeysPrompt = `Count the number of key icons visible in this game inventory image.

The keys are displayed in a horizontal row. Each key is in its own square tile.

Return ONLY a JSON object with the count:
{"count":7}

Count ALL visible key tiles, whether or not they have a number shown.`

// handleAnalyzeQuests analyzes a quest screenshot and extracts boss kill requirements
func (s *Server) handleAnalyzeQuests(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if s.openaiClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Image analysis not configured"})
		return
	}

	// Parse multipart form (max 2MB)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Failed to parse form: " + err.Error()})
		return
	}

	// Get the image file
	file, header, err := r.FormFile("image")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "No image file provided"})
		return
	}
	defer file.Close()

	// Check file size (max 2MB)
	if header.Size > 2<<20 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Image too large. Maximum size is 2MB."})
		return
	}

	// Read the image data (with limit as safety)
	imageData, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Failed to read image"})
		return
	}

	// Determine image type
	imageType := header.Header.Get("Content-Type")
	if imageType == "" {
		imageType = "image/png" // Default to PNG
	}

	// Encode to base64
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// Call OpenAI with JSON mode
	resp, err := s.openaiClient.ChatCompletionWithImageJSON(
		questAnalysisPrompt,
		"Please analyze this quest tracker screenshot and extract the boss kill requirements.",
		imageBase64,
		imageType,
		true, // Force JSON output
	)
	if err != nil {
		s.logger.Error("Failed to analyze quest screenshot", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Failed to analyze image: " + err.Error()})
		return
	}

	// Parse LLM response
	content := extractJSON(resp.Choices[0].Message.Content)
	var llmResp llmQuestResponse
	if err := json.Unmarshal([]byte(content), &llmResp); err != nil {
		s.logger.Error("Failed to parse LLM response",
			zap.Error(err),
			zap.String("content", resp.Choices[0].Message.Content))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Failed to parse analysis result"})
		return
	}

	// Validate and convert results
	results := make([]BossKillResult, 0)
	for _, b := range llmResp.Bosses {
		bossName := strings.ToLower(strings.TrimSpace(b.Name))
		if quests.IsValidBoss(bossName) && b.Kills > 0 {
			results = append(results, BossKillResult{
				Name:  bossName,
				Kills: b.Kills,
			})
		}
	}

	response := AnalyzeQuestsResponse{
		Bosses:  results,
		Applied: false,
	}

	// Check if we should apply the results
	if r.URL.Query().Get("apply") == "true" {
		playerName := r.URL.Query().Get("player")
		if playerName == "" {
			// Use the user's main player name
			playerName, err = s.db.GetPlayerName(r.Context(), session.UserID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(AnalyzeQuestsResponse{
					Bosses: results,
					Error:  "No player specified and no main player registered",
				})
				return
			}
		}

		// Verify the player belongs to this user
		if !s.userOwnsPlayer(r.Context(), session.UserID, playerName) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(AnalyzeQuestsResponse{
				Bosses: results,
				Error:  "You don't own this player",
			})
			return
		}

		// Apply the quest updates
		week, year := getWeekAndYear()
		for _, boss := range results {
			if err := s.db.UpsertQuest(r.Context(), session.UserID, playerName, week, year, boss.Name, boss.Kills); err != nil {
				s.logger.Error("Failed to apply quest update",
					zap.Error(err),
					zap.String("boss", boss.Name),
					zap.Int("kills", boss.Kills))
			}
		}

		response.Applied = true
		s.NotifyDataChange("quest")

		s.logger.Info("Applied quest updates from screenshot",
			zap.String("user_id", session.UserID),
			zap.String("player", playerName),
			zap.Int("bosses_updated", len(results)))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleAnalyzeKeys analyzes a key inventory screenshot and extracts key counts
func (s *Server) handleAnalyzeKeys(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if s.openaiClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Image analysis not configured"})
		return
	}

	// Parse multipart form (max 2MB)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Failed to parse form: " + err.Error()})
		return
	}

	// Get the image file
	file, header, err := r.FormFile("image")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "No image file provided"})
		return
	}
	defer file.Close()

	// Check file size (max 2MB)
	if header.Size > 2<<20 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Image too large. Maximum size is 2MB."})
		return
	}

	// Read the image data (with limit as safety)
	imageData, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Failed to read image"})
		return
	}

	// Determine image type
	imageType := header.Header.Get("Content-Type")
	if imageType == "" {
		imageType = "image/png" // Default to PNG
	}

	// Encode to base64
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// Call OpenAI with JSON mode - use reference images if available
	var resp *openai.ChatResponse
	if s.keyReferenceImages != nil && s.keyReferenceImages.HasImages() {
		resp, err = s.openaiClient.ChatCompletionWithReferences(
			keyAnalysisPromptWithRefs,
			"Analyze this key inventory screenshot. Match keys to the reference images provided.",
			s.keyReferenceImages.GetImages(),
			imageBase64,
			imageType,
			true, // Force JSON output
		)
	} else {
		resp, err = s.openaiClient.ChatCompletionWithImageJSON(
			keyAnalysisPrompt,
			"Please analyze this key inventory screenshot and extract the key counts.",
			imageBase64,
			imageType,
			true, // Force JSON output
		)
	}
	if err != nil {
		s.logger.Error("Failed to analyze key screenshot", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Failed to analyze image: " + err.Error()})
		return
	}

	// Parse LLM response
	content := extractJSON(resp.Choices[0].Message.Content)
	var llmResp llmKeyResponse
	if err := json.Unmarshal([]byte(content), &llmResp); err != nil {
		s.logger.Error("Failed to parse LLM response",
			zap.Error(err),
			zap.String("content", resp.Choices[0].Message.Content))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Failed to parse analysis result"})
		return
	}

	// Validate and convert results
	// Filter out invalid counts (> 300 is likely misdetection of non-key items)
	results := make([]KeyCountResult, 0)
	for _, k := range llmResp.Keys {
		keyType, ok := quests.ResolveKeyType(k.Type)
		if ok && k.Count > 0 && k.Count <= 300 {
			results = append(results, KeyCountResult{
				Type:  keyType,
				Count: k.Count,
			})
		}
	}

	response := AnalyzeKeysResponse{
		Keys:    results,
		Applied: false,
	}

	// Check if we should apply the results
	if r.URL.Query().Get("apply") == "true" {
		playerName := r.URL.Query().Get("player")
		if playerName == "" {
			// Use the user's main player name
			playerName, err = s.db.GetPlayerName(r.Context(), session.UserID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(AnalyzeKeysResponse{
					Keys:  results,
					Error: "No player specified and no main player registered",
				})
				return
			}
		}

		// Verify the player belongs to this user
		if !s.userOwnsPlayer(r.Context(), session.UserID, playerName) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(AnalyzeKeysResponse{
				Keys:  results,
				Error: "You don't own this player",
			})
			return
		}

		// Apply the key updates
		for _, key := range results {
			if err := s.db.UpsertPlayerKeys(r.Context(), playerName, key.Type, key.Count); err != nil {
				s.logger.Error("Failed to apply key update",
					zap.Error(err),
					zap.String("key_type", key.Type),
					zap.Int("count", key.Count))
			}
		}

		response.Applied = true
		s.NotifyDataChange("keys")

		s.logger.Info("Applied key updates from screenshot",
			zap.String("user_id", session.UserID),
			zap.String("player", playerName),
			zap.Int("keys_updated", len(results)))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// extractJSON attempts to extract a JSON object from a string that might contain extra text
func extractJSON(s string) string {
	// Find the first { and last }
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// cleanKeyJSON attempts to fix common JSON issues in LLM responses
func cleanKeyJSON(s string) string {
	// First extract the JSON
	s = extractJSON(s)

	// Try to parse as-is first
	var test llmKeyResponse
	if json.Unmarshal([]byte(s), &test) == nil {
		return s
	}

	// Find the keys array and extract just the valid entries
	keysStart := strings.Index(s, `"keys"`)
	if keysStart < 0 {
		return s
	}

	arrayStart := strings.Index(s[keysStart:], "[")
	if arrayStart < 0 {
		return s
	}
	arrayStart += keysStart

	// Find matching ]
	depth := 0
	arrayEnd := -1
	for i := arrayStart; i < len(s); i++ {
		if s[i] == '[' {
			depth++
		} else if s[i] == ']' {
			depth--
			if depth == 0 {
				arrayEnd = i
				break
			}
		}
	}

	if arrayEnd < 0 {
		return s
	}

	// Extract individual key objects
	arrayContent := s[arrayStart+1 : arrayEnd]
	var validKeys []string

	// Find each {...} object
	objStart := -1
	depth = 0
	for i := 0; i < len(arrayContent); i++ {
		if arrayContent[i] == '{' {
			if depth == 0 {
				objStart = i
			}
			depth++
		} else if arrayContent[i] == '}' {
			depth--
			if depth == 0 && objStart >= 0 {
				obj := arrayContent[objStart : i+1]
				// Try to parse this object
				var keyObj struct {
					Type  string `json:"type"`
					Count int    `json:"count"`
				}
				if json.Unmarshal([]byte(obj), &keyObj) == nil && keyObj.Type != "" {
					validKeys = append(validKeys, obj)
				}
				objStart = -1
			}
		}
	}

	if len(validKeys) > 0 {
		return `{"keys":[` + strings.Join(validKeys, ",") + `]}`
	}

	return s
}

// parseKeyResponseFallback tries to extract key type and count from verbose LLM responses
func parseKeyResponseFallback(content string) *singleKeyResponse {
	content = strings.ToLower(content)

	// List of key types to look for
	keyTypes := []string{"mountain", "godly", "burning", "underworld", "mutated", "stone", "ancient", "otherworldly"}

	var foundType string
	for _, kt := range keyTypes {
		if strings.Contains(content, kt) {
			foundType = kt
			break
		}
	}

	if foundType == "" {
		return nil
	}

	// Try to find a number - look for patterns like "41", "count": 41, "count is 5"
	var foundCount int
	// Look for numbers in the text
	words := strings.Fields(content)
	for _, word := range words {
		// Clean the word of punctuation
		cleaned := strings.Trim(word, ".,;:\"'()")
		if n, err := strconv.Atoi(cleaned); err == nil && n > 0 && n < 1000 {
			foundCount = n
			break
		}
	}

	return &singleKeyResponse{
		Type:  foundType,
		Count: foundCount,
	}
}

// Admin versions of analyze handlers (no auth required - internal network only)

// handleAdminAnalyzeQuests is the admin version that doesn't require authentication
func (s *Server) handleAdminAnalyzeQuests(w http.ResponseWriter, r *http.Request) {
	// Same logic as handleAnalyzeQuests but without auth check
	if s.openaiClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Image analysis not configured"})
		return
	}

	// Parse multipart form (max 2MB)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Failed to parse form: " + err.Error()})
		return
	}

	// Get the image file
	file, header, err := r.FormFile("image")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "No image file provided"})
		return
	}
	defer file.Close()

	// Check file size (max 2MB)
	if header.Size > 2<<20 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Image too large. Maximum size is 2MB."})
		return
	}

	// Read the image data (with limit as safety)
	imageData, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Failed to read image"})
		return
	}

	// Determine image type
	imageType := header.Header.Get("Content-Type")
	if imageType == "" {
		imageType = "image/png" // Default to PNG
	}

	// Encode to base64
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// Call OpenAI with JSON mode
	resp, err := s.openaiClient.ChatCompletionWithImageJSON(
		questAnalysisPrompt,
		"Please analyze this quest tracker screenshot and extract the boss kill requirements.",
		imageBase64,
		imageType,
		true, // Force JSON output
	)
	if err != nil {
		s.logger.Error("Failed to analyze quest screenshot", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Failed to analyze image: " + err.Error()})
		return
	}

	// Parse LLM response
	content := extractJSON(resp.Choices[0].Message.Content)
	var llmResp llmQuestResponse
	if err := json.Unmarshal([]byte(content), &llmResp); err != nil {
		s.logger.Error("Failed to parse LLM response",
			zap.Error(err),
			zap.String("content", resp.Choices[0].Message.Content))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeQuestsResponse{Error: "Failed to parse analysis result"})
		return
	}

	// Validate and convert results
	results := make([]BossKillResult, 0)
	for _, b := range llmResp.Bosses {
		bossName := strings.ToLower(strings.TrimSpace(b.Name))
		if quests.IsValidBoss(bossName) && b.Kills > 0 {
			results = append(results, BossKillResult{
				Name:  bossName,
				Kills: b.Kills,
			})
		}
	}

	response := AnalyzeQuestsResponse{
		Bosses:  results,
		Applied: false,
	}

	// Admin version doesn't auto-apply, just returns results
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleAdminAnalyzeKeys is the admin version that doesn't require authentication
// It analyzes the full image in a single pass using OpenAI
func (s *Server) handleAdminAnalyzeKeys(w http.ResponseWriter, r *http.Request) {
	if s.openaiClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Image analysis not configured"})
		return
	}

	// Parse multipart form (max 2MB)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Failed to parse form: " + err.Error()})
		return
	}

	// Get the image file
	file, header, err := r.FormFile("image")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "No image file provided"})
		return
	}
	defer file.Close()

	// Check file size (max 2MB)
	if header.Size > 2<<20 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Image too large. Maximum size is 2MB."})
		return
	}

	// Read the image data (with limit as safety)
	imageData, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Failed to read image"})
		return
	}

	// Get image dimensions for logging
	img, _, _ := image.Decode(bytes.NewReader(imageData))
	var imgWidth, imgHeight int
	if img != nil {
		imgWidth = img.Bounds().Dx()
		imgHeight = img.Bounds().Dy()
	}

	s.logger.Info("Starting key analysis",
		zap.Int("img_width", imgWidth),
		zap.Int("img_height", imgHeight))

	// Determine image type
	imageType := header.Header.Get("Content-Type")
	if imageType == "" {
		imageType = "image/png" // Default to PNG
	}

	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// Single pass with JSON mode using OpenAI - use reference images if available
	var resp *openai.ChatResponse
	if s.keyReferenceImages != nil && s.keyReferenceImages.HasImages() {
		s.logger.Info("Using reference images for key analysis",
			zap.Int("ref_count", len(s.keyReferenceImages.GetImages())))
		resp, err = s.openaiClient.ChatCompletionWithReferences(
			keyAnalysisPromptWithRefs,
			"Analyze this key inventory screenshot. Match keys to the reference images provided.",
			s.keyReferenceImages.GetImages(),
			imageBase64,
			imageType,
			true, // Force JSON output
		)
	} else {
		resp, err = s.openaiClient.ChatCompletionWithImageJSON(
			keyAnalysisPrompt,
			"Please analyze this key inventory screenshot and extract the key counts.",
			imageBase64,
			imageType,
			true, // Force JSON output
		)
	}
	if err != nil {
		s.logger.Error("Failed to analyze keys", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Failed to analyze image: " + err.Error()})
		return
	}

	content := extractJSON(resp.Choices[0].Message.Content)
	s.logger.Info("Key analysis response", zap.String("content", content))

	var llmResp llmKeyResponse
	if err := json.Unmarshal([]byte(content), &llmResp); err != nil {
		s.logger.Error("Failed to parse JSON response",
			zap.Error(err),
			zap.String("content", resp.Choices[0].Message.Content))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AnalyzeKeysResponse{Error: "Failed to parse analysis result"})
		return
	}

	// Validate and convert results
	// Filter out invalid counts (> 300 is likely misdetection of non-key items)
	results := make([]KeyCountResult, 0)
	for _, k := range llmResp.Keys {
		keyType, ok := quests.ResolveKeyType(k.Type)
		if ok && k.Count > 0 && k.Count <= 300 {
			results = append(results, KeyCountResult{
				Type:  keyType,
				Count: k.Count,
			})
		}
	}

	s.logger.Info("Key analysis complete", zap.Int("keys_found", len(results)))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AnalyzeKeysResponse{Keys: results, Applied: false})
}

// analyzeKeyTiles analyzes a slice of key tile images and returns the results
func (s *Server) analyzeKeyTiles(tiles [][]byte, splitInfo *ImageSplitInfo) []KeyCountResult {
	if splitInfo != nil {
		s.logger.Info("Analyzing tiles",
			zap.Int("tile_count", len(tiles)),
			zap.Int("tile_size", splitInfo.TileSize))
	} else {
		s.logger.Info("Analyzing tiles", zap.Int("tile_count", len(tiles)))
	}

	results := make([]KeyCountResult, 0)
	for i, tileData := range tiles {
		tileBase64 := base64.StdEncoding.EncodeToString(tileData)

		resp, err := s.openaiClient.ChatCompletionWithImageJSON(
			singleKeyAnalysisPrompt,
			"Identify the key type and count.",
			tileBase64,
			"image/png",
			true, // Force JSON output
		)
		if err != nil {
			s.logger.Warn("Failed to analyze tile",
				zap.Int("tile", i),
				zap.Error(err))
			continue
		}

		// Parse the response - try JSON first, then fallback
		rawContent := resp.Choices[0].Message.Content
		content := extractJSON(rawContent)
		var keyResp *singleKeyResponse

		var parsed singleKeyResponse
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			// JSON parsing failed, try fallback parser
			keyResp = parseKeyResponseFallback(rawContent)
			if keyResp != nil {
				s.logger.Info("Used fallback parser for tile",
					zap.Int("tile", i),
					zap.String("type", keyResp.Type),
					zap.Int("count", keyResp.Count),
					zap.String("raw", rawContent))
			} else {
				s.logger.Warn("Failed to parse tile response",
					zap.Int("tile", i),
					zap.Error(err),
					zap.String("content", rawContent))
				continue
			}
		} else {
			keyResp = &parsed
			s.logger.Info("Analyzed tile",
				zap.Int("tile", i),
				zap.String("type", keyResp.Type),
				zap.Int("count", keyResp.Count))
		}

		// Only add if we got a valid key type and count > 0 and <= 300
		// (counts over 300 are likely misdetection of non-key items)
		if keyResp != nil && keyResp.Count > 0 && keyResp.Count <= 300 {
			keyType, ok := quests.ResolveKeyType(keyResp.Type)
			if ok {
				results = append(results, KeyCountResult{
					Type:  keyType,
					Count: keyResp.Count,
				})
			}
		}
	}

	return results
}
