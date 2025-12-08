package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jirwin/idleclans/pkg/quests"
	"go.uber.org/zap"
)

// PartyRequest represents a request to create a party
type PartyRequest struct {
	Players []string `json:"players"`
}

// PartyResponse represents the full party state returned to clients
type PartyResponse struct {
	ID               string                          `json:"id"`
	Players          []string                        `json:"players"`
	Plan             PlanData                        `json:"plan"`
	CurrentStepIndex int                             `json:"current_step_index"`
	StepProgress     []PartyStepProgressResponse     `json:"step_progress"`
	PlayerQuests     map[string][]Quest              `json:"player_quests"` // player name -> quests
	StartedAt        *time.Time                      `json:"started_at"`
	EndedAt          *time.Time                      `json:"ended_at"`
	CreatedAt        time.Time                       `json:"created_at"`
}

// PartyStepProgressResponse represents step progress in API responses
type PartyStepProgressResponse struct {
	StepIndex    int        `json:"step_index"`
	BossName     string     `json:"boss_name"`
	KillsTracked int        `json:"kills_tracked"`
	KeysUsed     int        `json:"keys_used"`
	StartedAt    *time.Time `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
}

// UpdateKillsRequest represents a request to update kills
type UpdateKillsRequest struct {
	Kills        int  `json:"kills"`
	Delta        bool `json:"delta"`          // If true, add/subtract from current; if false, set absolute value
	ExpectedKills *int `json:"expected_kills"` // Optional: if provided, validates current value matches before applying
}

// UpdateKeysRequest represents a request to update keys used
type UpdateKeysUsedRequest struct {
	KeysUsed int `json:"keys_used"`
}

// generatePartyID generates a random party ID
func generatePartyID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// handleCreateParty creates a new party session
func (s *Server) handleCreateParty(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req PartyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Players) == 0 {
		http.Error(w, "No players specified", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Generate the plan for these players
	week, year := getWeekAndYear()
	planner := quests.NewPlanner(s.db)
	plan, err := planner.GeneratePlanFiltered(ctx, week, year, req.Players)
	if err != nil {
		s.logger.Error("Failed to generate plan", zap.Error(err))
		http.Error(w, "Failed to generate plan", http.StatusInternalServerError)
		return
	}

	// Convert plan to API format
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

	planData := PlanData{
		Week:      week,
		Year:      year,
		Parties:   parties,
		Leftovers: leftovers,
	}

	// Serialize plan data
	planJSON, err := json.Marshal(planData)
	if err != nil {
		s.logger.Error("Failed to serialize plan", zap.Error(err))
		http.Error(w, "Failed to create party", http.StatusInternalServerError)
		return
	}

	// Serialize players
	playersJSON, err := json.Marshal(req.Players)
	if err != nil {
		s.logger.Error("Failed to serialize players", zap.Error(err))
		http.Error(w, "Failed to create party", http.StatusInternalServerError)
		return
	}

	// Create party in database
	partyID := generatePartyID()
	if err := s.db.CreateParty(ctx, partyID, string(playersJSON), string(planJSON)); err != nil {
		s.logger.Error("Failed to create party", zap.Error(err))
		http.Error(w, "Failed to create party", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Party created",
		zap.String("party_id", partyID),
		zap.Strings("players", req.Players),
	)

	// Send Discord notification if configured
	if s.discordSender != nil && s.config.DiscordChannelID != "" {
		// Build ping string for party members
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
		pingContent := strings.Join(pings, " ")

		// Build party URL
		partyURL := fmt.Sprintf("%s/party/%s", s.config.BaseURL, partyID)

		// Create embed
		embed := &DiscordEmbed{
			Title:       "Party Started!",
			Description: fmt.Sprintf("A party has been started with: %s", strings.Join(req.Players, ", ")),
			Color:       0x5865F2, // Discord blurple
			Fields: []DiscordEmbedField{
				{
					Name:   "Party Link",
					Value:  fmt.Sprintf("[Join Party](%s)", partyURL),
					Inline: false,
				},
			},
		}

		// Send notification (don't fail party creation if Discord fails)
		if err := s.discordSender.SendMessageWithEmbed(s.config.DiscordChannelID, pingContent, embed); err != nil {
			s.logger.Warn("Failed to send party notification to Discord",
				zap.Error(err),
				zap.String("party_id", partyID))
		} else {
			s.logger.Info("Sent party notification to Discord",
				zap.String("party_id", partyID),
				zap.Strings("players", req.Players))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": partyID})
}

// handleGetUserParties returns parties the authenticated user has been part of
func (s *Server) handleGetUserParties(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	// Get parties for this user (limit to 6: 1 current + 5 previous)
	parties, err := s.db.GetPartiesForUser(ctx, session.UserID, 6)
	if err != nil {
		s.logger.Error("Failed to get user parties", zap.Error(err))
		http.Error(w, "Failed to get parties", http.StatusInternalServerError)
		return
	}

	// Convert to API format
	partyResponses := make([]PartySummaryResponse, len(parties))
	for i, party := range parties {
		var players []string
		if err := json.Unmarshal([]byte(party.Players), &players); err != nil {
			s.logger.Warn("Failed to parse players", zap.String("party_id", party.ID), zap.Error(err))
			players = []string{}
		}

		partyResponses[i] = PartySummaryResponse{
			ID:        party.ID,
			Players:   players,
			StartedAt: party.StartedAt,
			EndedAt:   party.EndedAt,
			CreatedAt: party.CreatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(partyResponses)
}

// PartySummaryResponse represents a summary of a party for listing
type PartySummaryResponse struct {
	ID        string     `json:"id"`
	Players   []string   `json:"players"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// handleGetParty returns the full party state
func (s *Server) handleGetParty(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	partyID := r.PathValue("partyId")
	if partyID == "" {
		http.Error(w, "Missing party ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	party, err := s.db.GetParty(ctx, partyID)
	if err != nil {
		s.logger.Error("Failed to get party", zap.Error(err))
		http.Error(w, "Failed to get party", http.StatusInternalServerError)
		return
	}
	if party == nil {
		http.Error(w, "Party not found", http.StatusNotFound)
		return
	}

	// Parse players and plan
	var players []string
	if err := json.Unmarshal([]byte(party.Players), &players); err != nil {
		s.logger.Error("Failed to parse players", zap.Error(err))
		http.Error(w, "Failed to get party", http.StatusInternalServerError)
		return
	}

	// Verify user is authorized to view this party (owns a character in the party)
	if !s.userCanAccessParty(ctx, session.UserID, players) {
		http.Error(w, "You don't have a character in this party", http.StatusForbidden)
		return
	}

	var planData PlanData
	if err := json.Unmarshal([]byte(party.PlanData), &planData); err != nil {
		s.logger.Error("Failed to parse plan data", zap.Error(err))
		http.Error(w, "Failed to get party", http.StatusInternalServerError)
		return
	}

	// Get step progress
	progress, err := s.db.GetAllPartyStepProgress(ctx, partyID)
	if err != nil {
		s.logger.Error("Failed to get step progress", zap.Error(err))
		http.Error(w, "Failed to get party", http.StatusInternalServerError)
		return
	}

	stepProgress := make([]PartyStepProgressResponse, len(progress))
	for i, p := range progress {
		stepProgress[i] = PartyStepProgressResponse{
			StepIndex:    p.StepIndex,
			BossName:     p.BossName,
			KillsTracked: p.KillsTracked,
			KeysUsed:     p.KeysUsed,
			StartedAt:    p.StartedAt,
			CompletedAt:  p.CompletedAt,
		}
	}

	// Fetch quest data for all players in the party
	week, year := getWeekAndYear()
	playerQuests := make(map[string][]Quest)
	for _, playerName := range players {
		quests, err := s.db.GetPlayerQuests(ctx, playerName, week, year)
		if err != nil {
			s.logger.Warn("Failed to get quests for player", zap.String("player", playerName), zap.Error(err))
			continue
		}
		apiQuests := make([]Quest, len(quests))
		for i, q := range quests {
			apiQuests[i] = Quest{
				BossName:      q.BossName,
				RequiredKills: q.RequiredKills,
				CurrentKills:  q.CurrentKills,
			}
		}
		playerQuests[playerName] = apiQuests
	}

	response := PartyResponse{
		ID:               party.ID,
		Players:          players,
		Plan:             planData,
		CurrentStepIndex: party.CurrentStepIndex,
		StepProgress:     stepProgress,
		PlayerQuests:     playerQuests,
		StartedAt:        party.StartedAt,
		EndedAt:          party.EndedAt,
		CreatedAt:        party.CreatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleStartPartyStep starts the first step (or current step) of the party
func (s *Server) handleStartPartyStep(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	partyID := r.PathValue("partyId")
	if partyID == "" {
		http.Error(w, "Missing party ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	party, players, err := s.getPartyAndPlayers(ctx, partyID)
	if err != nil {
		s.logger.Error("Failed to get party", zap.Error(err))
		http.Error(w, "Failed to get party", http.StatusInternalServerError)
		return
	}
	if party == nil {
		http.Error(w, "Party not found", http.StatusNotFound)
		return
	}

	if !s.userCanAccessParty(ctx, session.UserID, players) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if party.EndedAt != nil {
		http.Error(w, "Party has ended", http.StatusBadRequest)
		return
	}

	// Start the party if not started
	if party.StartedAt == nil {
		if err := s.db.StartParty(ctx, partyID); err != nil {
			s.logger.Error("Failed to start party", zap.Error(err))
			http.Error(w, "Failed to start party", http.StatusInternalServerError)
			return
		}
	}

	// Get current step info from plan
	var planData PlanData
	if err := json.Unmarshal([]byte(party.PlanData), &planData); err != nil {
		s.logger.Error("Failed to parse plan", zap.Error(err))
		http.Error(w, "Failed to start step", http.StatusInternalServerError)
		return
	}

	// Calculate total steps (flatten all tasks from all parties)
	allTasks := s.getAllTasksFromPlan(planData)
	if party.CurrentStepIndex >= len(allTasks) {
		http.Error(w, "No more steps", http.StatusBadRequest)
		return
	}

	currentTask := allTasks[party.CurrentStepIndex]

	// Start the step
	if err := s.db.StartPartyStep(ctx, partyID, party.CurrentStepIndex, currentTask.BossName); err != nil {
		s.logger.Error("Failed to start step", zap.Error(err))
		http.Error(w, "Failed to start step", http.StatusInternalServerError)
		return
	}

	s.NotifyDataChange("party:" + partyID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

// handleUpdatePartyKills updates the kill count for the current step
func (s *Server) handleUpdatePartyKills(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	partyID := r.PathValue("partyId")
	if partyID == "" {
		http.Error(w, "Missing party ID", http.StatusBadRequest)
		return
	}

	var req UpdateKillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	s.logger.Info("Party kill update request",
		zap.String("party_id", partyID),
		zap.String("user_id", session.UserID),
		zap.Int("kills", req.Kills),
		zap.Bool("delta", req.Delta),
		zap.Any("expected_kills", req.ExpectedKills))

	ctx := r.Context()

	party, players, err := s.getPartyAndPlayers(ctx, partyID)
	if err != nil {
		s.logger.Error("Failed to get party", zap.Error(err))
		http.Error(w, "Failed to get party", http.StatusInternalServerError)
		return
	}
	if party == nil {
		http.Error(w, "Party not found", http.StatusNotFound)
		return
	}

	if !s.userCanAccessParty(ctx, session.UserID, players) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if party.EndedAt != nil {
		http.Error(w, "Party has ended", http.StatusBadRequest)
		return
	}

	// Get current step info
	var planData PlanData
	if err := json.Unmarshal([]byte(party.PlanData), &planData); err != nil {
		s.logger.Error("Failed to parse plan", zap.Error(err))
		http.Error(w, "Failed to update kills", http.StatusInternalServerError)
		return
	}

	allTasks := s.getAllTasksFromPlan(planData)
	if party.CurrentStepIndex >= len(allTasks) {
		http.Error(w, "No current step", http.StatusBadRequest)
		return
	}

	currentTask := allTasks[party.CurrentStepIndex]

	// Get current step progress
	progress, err := s.db.GetPartyStepProgress(ctx, partyID, party.CurrentStepIndex)
	if err != nil {
		s.logger.Error("Failed to get step progress", zap.Error(err))
		http.Error(w, "Failed to update kills", http.StatusInternalServerError)
		return
	}

	var oldKills int
	if progress != nil {
		oldKills = progress.KillsTracked
	}

	// Validate expected kills if provided (optimistic concurrency control)
	if req.ExpectedKills != nil && *req.ExpectedKills != oldKills {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":        "conflict",
			"message":      "Kill count was modified by another user",
			"actual_kills": oldKills,
		})
		return
	}

	var newKills int
	if req.Delta {
		newKills = oldKills + req.Kills
	} else {
		newKills = req.Kills
	}

	if newKills < 0 {
		newKills = 0
	}

	// Update step progress
	keysUsed := 0
	if progress != nil {
		keysUsed = progress.KeysUsed
	}
	if err := s.db.UpsertPartyStepProgress(ctx, partyID, party.CurrentStepIndex, currentTask.BossName, newKills, keysUsed); err != nil {
		s.logger.Error("Failed to update step progress",
			zap.Error(err),
			zap.String("party_id", partyID),
			zap.Int("step_index", party.CurrentStepIndex),
			zap.String("boss", currentTask.BossName),
			zap.Int("kills", newKills))
		http.Error(w, "Failed to update kills", http.StatusInternalServerError)
		return
	}
	s.logger.Info("Updated party step progress",
		zap.String("party_id", partyID),
		zap.Int("step_index", party.CurrentStepIndex),
		zap.String("boss", currentTask.BossName),
		zap.Int("old_kills", oldKills),
		zap.Int("new_kills", newKills),
		zap.Strings("players", players))

	// Update quest current_kills for all players in the party who have this boss quest
	killsDelta := newKills - oldKills
	if killsDelta != 0 {
		week, year := getWeekAndYear()
		if err := s.db.IncrementQuestCurrentKills(ctx, players, currentTask.BossName, week, year, killsDelta); err != nil {
			s.logger.Error("Failed to update quest kills",
				zap.Error(err),
				zap.Strings("players", players),
				zap.String("boss", currentTask.BossName),
				zap.Int("delta", killsDelta),
				zap.Int("week", week),
				zap.Int("year", year))
			// Don't fail the request, but log detailed error for debugging
		} else {
			s.logger.Info("Updated quest kills",
				zap.Strings("players", players),
				zap.String("boss", currentTask.BossName),
				zap.Int("delta", killsDelta))
		}
	}

	s.NotifyDataChange("party:" + partyID)
	s.NotifyDataChange("quest") // Also notify quest changes

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"kills": newKills})
}

// handleUpdatePartyKeys updates the keys used for the current step
func (s *Server) handleUpdatePartyKeys(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	partyID := r.PathValue("partyId")
	if partyID == "" {
		http.Error(w, "Missing party ID", http.StatusBadRequest)
		return
	}

	var req UpdateKeysUsedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	party, players, err := s.getPartyAndPlayers(ctx, partyID)
	if err != nil {
		s.logger.Error("Failed to get party", zap.Error(err))
		http.Error(w, "Failed to get party", http.StatusInternalServerError)
		return
	}
	if party == nil {
		http.Error(w, "Party not found", http.StatusNotFound)
		return
	}

	if !s.userCanAccessParty(ctx, session.UserID, players) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if party.EndedAt != nil {
		http.Error(w, "Party has ended", http.StatusBadRequest)
		return
	}

	// Verify user is the current step leader (or owns the leader as alt)
	var planData PlanData
	if err := json.Unmarshal([]byte(party.PlanData), &planData); err != nil {
		s.logger.Error("Failed to parse plan", zap.Error(err))
		http.Error(w, "Failed to update keys", http.StatusInternalServerError)
		return
	}

	allTasks := s.getAllTasksFromPlan(planData)
	if party.CurrentStepIndex >= len(allTasks) {
		http.Error(w, "No current step", http.StatusBadRequest)
		return
	}

	currentTask := allTasks[party.CurrentStepIndex]

	// Check if user owns the key holder
	if currentTask.KeyHolder != "" && !s.userOwnsPlayer(ctx, session.UserID, currentTask.KeyHolder) {
		http.Error(w, "Only the key holder can update keys used", http.StatusForbidden)
		return
	}

	if err := s.db.UpdatePartyStepKeys(ctx, partyID, party.CurrentStepIndex, req.KeysUsed); err != nil {
		s.logger.Error("Failed to update keys", zap.Error(err))
		http.Error(w, "Failed to update keys", http.StatusInternalServerError)
		return
	}

	s.NotifyDataChange("party:" + partyID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"keys_used": req.KeysUsed})
}

// handleNextPartyStep advances to the next step
func (s *Server) handleNextPartyStep(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	partyID := r.PathValue("partyId")
	if partyID == "" {
		http.Error(w, "Missing party ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	party, players, err := s.getPartyAndPlayers(ctx, partyID)
	if err != nil {
		s.logger.Error("Failed to get party", zap.Error(err))
		http.Error(w, "Failed to get party", http.StatusInternalServerError)
		return
	}
	if party == nil {
		http.Error(w, "Party not found", http.StatusNotFound)
		return
	}

	if !s.userCanAccessParty(ctx, session.UserID, players) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if party.EndedAt != nil {
		http.Error(w, "Party has ended", http.StatusBadRequest)
		return
	}

	var planData PlanData
	if err := json.Unmarshal([]byte(party.PlanData), &planData); err != nil {
		s.logger.Error("Failed to parse plan", zap.Error(err))
		http.Error(w, "Failed to advance step", http.StatusInternalServerError)
		return
	}

	allTasks := s.getAllTasksFromPlan(planData)
	nextStepIndex := party.CurrentStepIndex + 1

	if nextStepIndex >= len(allTasks) {
		http.Error(w, "No more steps", http.StatusBadRequest)
		return
	}

	// Mark current step as completed
	if err := s.db.CompletePartyStep(ctx, partyID, party.CurrentStepIndex); err != nil {
		s.logger.Error("Failed to complete step", zap.Error(err))
		// Continue anyway
	}

	// Update step index
	if err := s.db.UpdatePartyStepIndex(ctx, partyID, nextStepIndex); err != nil {
		s.logger.Error("Failed to update step index", zap.Error(err))
		http.Error(w, "Failed to advance step", http.StatusInternalServerError)
		return
	}

	// Start the new step
	nextTask := allTasks[nextStepIndex]
	if err := s.db.StartPartyStep(ctx, partyID, nextStepIndex, nextTask.BossName); err != nil {
		s.logger.Error("Failed to start new step", zap.Error(err))
		// Continue anyway - step index is already updated
	}

	s.NotifyDataChange("party:" + partyID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"current_step_index": nextStepIndex})
}

// handleEndParty ends the party session
func (s *Server) handleEndParty(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	partyID := r.PathValue("partyId")
	if partyID == "" {
		http.Error(w, "Missing party ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	party, players, err := s.getPartyAndPlayers(ctx, partyID)
	if err != nil {
		s.logger.Error("Failed to get party", zap.Error(err))
		http.Error(w, "Failed to get party", http.StatusInternalServerError)
		return
	}
	if party == nil {
		http.Error(w, "Party not found", http.StatusNotFound)
		return
	}

	if !s.userCanAccessParty(ctx, session.UserID, players) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if party.EndedAt != nil {
		http.Error(w, "Party already ended", http.StatusBadRequest)
		return
	}

	// Complete current step if in progress
	if err := s.db.CompletePartyStep(ctx, partyID, party.CurrentStepIndex); err != nil {
		s.logger.Error("Failed to complete current step", zap.Error(err))
		// Continue anyway
	}

	if err := s.db.EndParty(ctx, partyID); err != nil {
		s.logger.Error("Failed to end party", zap.Error(err))
		http.Error(w, "Failed to end party", http.StatusInternalServerError)
		return
	}

	s.NotifyDataChange("party:" + partyID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ended"})
}

// Helper functions

// getPartyAndPlayers retrieves a party and parses its players
func (s *Server) getPartyAndPlayers(ctx interface{ Value(any) any }, partyID string) (*quests.PartySession, []string, error) {
	party, err := s.db.GetParty(ctx.(interface {
		Value(any) any
		Done() <-chan struct{}
		Err() error
		Deadline() (time.Time, bool)
	}), partyID)
	if err != nil {
		return nil, nil, err
	}
	if party == nil {
		return nil, nil, nil
	}

	var players []string
	if err := json.Unmarshal([]byte(party.Players), &players); err != nil {
		return nil, nil, err
	}

	return party, players, nil
}

// userCanAccessParty checks if a user owns any character in the party
func (s *Server) userCanAccessParty(ctx interface{ Value(any) any }, discordID string, players []string) bool {
	for _, playerName := range players {
		if s.userOwnsPlayer(ctx.(interface {
			Value(any) any
			Done() <-chan struct{}
			Err() error
			Deadline() (time.Time, bool)
		}), discordID, playerName) {
			return true
		}
	}
	return false
}

// getAllTasksFromPlan flattens all tasks from all parties in the plan
func (s *Server) getAllTasksFromPlan(plan PlanData) []PlanPartyTask {
	var tasks []PlanPartyTask
	for _, party := range plan.Parties {
		tasks = append(tasks, party.Tasks...)
	}
	return tasks
}

