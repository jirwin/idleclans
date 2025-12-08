package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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

	// Collect unique Discord user IDs for pinging
	playerDiscordIDs := make(map[string]string)
	for _, playerName := range req.Players {
		discordID, err := s.db.GetDiscordUserIDForPlayer(ctx, playerName)
		if err == nil && discordID != "" {
			playerDiscordIDs[playerName] = discordID
		}
	}

	// Build ping string (unique users only)
	seenIDs := make(map[string]bool)
	var pings []string
	for _, discordID := range playerDiscordIDs {
		if !seenIDs[discordID] {
			seenIDs[discordID] = true
			pings = append(pings, fmt.Sprintf("<@%s>", discordID))
		}
	}
	pingContent := strings.Join(pings, " ")

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
		Title: "Time to do quests!",
		Color: 0xe91e63, // Pink
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


