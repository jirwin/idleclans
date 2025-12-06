package idleclans

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/jirwin/idleclans/pkg/bot"
	"github.com/jirwin/idleclans/pkg/quests"
	"go.uber.org/zap"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type questsHandler struct {
	db *quests.DB
}

func newQuestsHandler() (*questsHandler, error) {
	dbPath := "quests.db"
	db, err := quests.NewDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create quests database: %w", err)
	}

	return &questsHandler{db: db}, nil
}

func (h *questsHandler) close() error {
	return h.db.Close()
}

func (p *plugin) questsCmd(ctx context.Context) bot.MessageHandler {
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if p.questsHandler == nil {
			s.ChannelMessageSend(m.ChannelID, "Error: Quest system unavailable")
			return
		}
		if m.Author.ID == s.State.User.ID {
			return
		}

		// Check for all command aliases
		var content string
		if strings.HasPrefix(m.Content, "!quests") {
			content = strings.TrimSpace(strings.TrimPrefix(m.Content, "!quests"))
		} else if strings.HasPrefix(m.Content, "!quest") {
			content = strings.TrimSpace(strings.TrimPrefix(m.Content, "!quest"))
		} else if strings.HasPrefix(m.Content, "!q") {
			content = strings.TrimSpace(strings.TrimPrefix(m.Content, "!q"))
		} else {
			return
		}

		parts := strings.Fields(content)

		if len(parts) == 0 {
			// No command specified and no options - run clan command
			p.questsHandler.handleClan(ctx, s, m, parts)
			return
		}

		command := parts[0]

		// Check if the command is a boss name (before checking other commands)
		// Only treat it as a ping if there's no second argument that looks like a quest update
		if bossName, ok := quests.ResolveBossName(command); ok {
			// If there's a second argument that could be a number, it's likely a quest update
			if len(parts) > 1 {
				// Check if the second argument is a number (quest update format)
				if _, err := strconv.Atoi(parts[1]); err == nil {
					// This looks like a quest update, not a ping
					// Fall through to default case which handles quest updates
				} else {
					// Second argument is not a number, treat as boss ping
					p.questsHandler.handleBossPing(ctx, s, m, bossName)
					return
				}
			} else {
				// Only one argument (boss name), treat as ping
				p.questsHandler.handleBossPing(ctx, s, m, bossName)
				return
			}
		}

		switch command {
		case "help", "h":
			p.questsHandler.handleHelp(ctx, s, m)
		case "register":
			p.questsHandler.handleRegister(ctx, s, m, parts[1:])
		case "keys":
			p.questsHandler.handleKeys(ctx, s, m, parts[1:])
		case "clan":
			p.questsHandler.handleClan(ctx, s, m, parts[1:])
		case "ping":
			p.questsHandler.handlePing(ctx, s, m)
		case "complete":
			p.questsHandler.handleComplete(ctx, s, m, parts[1:])
		case "who":
			p.questsHandler.handleWho(ctx, s, m, parts[1:])
		default:
			// Additional input provided but not a known command - assume it's a quest update command
			p.questsHandler.handleUpdate(ctx, s, m, parts)
		}
	}
}

func (p *plugin) bossPingCmd(ctx context.Context) bot.MessageHandler {
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if p.questsHandler == nil {
			return // Quest system unavailable, silently ignore
		}
		if m.Author.ID == s.State.User.ID {
			return
		}

		// Check if message starts with ! and is a valid boss name
		if !strings.HasPrefix(m.Content, "!") {
			return
		}

		// Extract command (remove !)
		content := strings.TrimSpace(strings.TrimPrefix(m.Content, "!"))
		if content == "" {
			return
		}

		// Get first word (command name)
		parts := strings.Fields(content)
		if len(parts) == 0 {
			return
		}

		command := parts[0]

		// Check if it's a valid boss name (using ResolveBossName to support aliases)
		if bossName, ok := quests.ResolveBossName(command); ok {
			p.questsHandler.handleBossPing(ctx, s, m, bossName)
		}
	}
}

func (h *questsHandler) handleHelp(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {
	embed := &discordgo.MessageEmbed{
		Title:       "Quest Commands Help",
		Description: "Available commands for managing weekly quests",
		Color:       0x9b59b6, // Purple color
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Default (no command)",
				Value:  "`!quests` - Shows clan key requirements for current week",
				Inline: false,
			},
			{
				Name:   "Register Player",
				Value:  "`!quests register <player_name>` - Register your default player name",
				Inline: false,
			},
			{
				Name:   "Update Quests",
				Value:  "`!quests [player_name] <boss> <count> [boss] <count> ...`\nUpdate your weekly quests. Supports comma or space separated.\nBoss names can be full name, first letter, or key color.\nExample: `!quests griffin 45 hades 12` or `!quests g 45 h 12`",
				Inline: false,
			},
			{
				Name:   "View Keys",
				Value:  "`!quests keys [player_name] [week|date]` - View key requirements for a player\nExample: `!quests keys` or `!quests keys mekkyra 2025-01-15`",
				Inline: false,
			},
			{
				Name:   "Clan Keys",
				Value:  "`!quests clan [week|date]` - View total clan key requirements\nExample: `!quests clan` or `!quests clan 5`",
				Inline: false,
			},
			{
				Name:   "Complete Quest",
				Value:  "`!quests complete <boss> [amount]` - Mark a quest as complete or reduce by amount\nExample: `!quests complete griffin` or `!quests complete g 30`",
				Inline: false,
			},
			{
				Name:   "Ping Matching Players",
				Value:  "`!quests ping` - Ping players who have matching quests with you",
				Inline: false,
			},
			{
				Name:   "Who Has Keys",
				Value:  "`!quests who [week|date]` - Show each boss and who has how many keys\nExample: `!quests who` or `!quests who 5`",
				Inline: false,
			},
			{
				Name:   "Command Aliases",
				Value:  "You can use `!quests`, `!quest`, or `!q` for all commands",
				Inline: false,
			},
		},
	}

	s.ChannelMessageSendEmbeds(m.ChannelID, []*discordgo.MessageEmbed{embed})
}

func (h *questsHandler) handleRegister(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	l := ctxzap.Extract(ctx)

	if len(args) == 0 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!quests register <player_name>`")
		return
	}

	playerName := strings.Join(args, " ")
	err := h.db.RegisterPlayer(ctx, m.Author.ID, playerName)
	if err != nil {
		l.Error("Failed to register player", zap.Error(err))
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error registering player: %s", err.Error()))
		return
	}

	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Registered default player name: **%s**", playerName))
}

func (h *questsHandler) handleUpdate(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	l := ctxzap.Extract(ctx)

	if len(args) == 0 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!quests [player_name] <boss> <count> [boss] <count> ...` (supports comma or space separated)")
		return
	}

	// Check if input contains commas (comma-separated format)
	content := strings.Join(args, " ")
	hasCommas := strings.Contains(content, ",")

	// Determine player name
	var playerName string
	var bossArgs []string

	if hasCommas {
		// Comma-separated format: split by commas and process
		parts := strings.Split(content, ",")
		trimmedParts := make([]string, 0, len(parts)*2) // May expand if pairs are space-separated

		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			// Check if this part contains a space (boss count pair)
			if strings.Contains(part, " ") {
				// Split the pair: "griffin 43" -> ["griffin", "43"]
				pairParts := strings.Fields(part)
				trimmedParts = append(trimmedParts, pairParts...)
			} else {
				// Single value: boss or count
				trimmedParts = append(trimmedParts, part)
			}
		}

		// Check if first part is a valid boss name (using ResolveBossName to support aliases)
		if _, ok := quests.ResolveBossName(trimmedParts[0]); ok {
			// First part is a boss, so use default player name
			var err error
			playerName, err = h.db.GetPlayerName(ctx, m.Author.ID)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "No default player name registered. Use `!quests register <player_name>` first, or provide player name: `!quests <player_name> <boss> <count> ...`")
				return
			}
			bossArgs = trimmedParts
		} else {
			// First part is player name
			playerName = trimmedParts[0]
			bossArgs = trimmedParts[1:]
		}
	} else {
		// Space-separated format (original)
		// Check if first arg is a valid boss name (using ResolveBossName to support aliases)
		if _, ok := quests.ResolveBossName(args[0]); ok {
			// First arg is a boss, so use default player name
			var err error
			playerName, err = h.db.GetPlayerName(ctx, m.Author.ID)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "No default player name registered. Use `!quests register <player_name>` first, or provide player name: `!quests <player_name> <boss> <count> ...`")
				return
			}
			bossArgs = args
		} else {
			// First arg is player name
			playerName = args[0]
			bossArgs = args[1:]
		}
	}

	if len(bossArgs) == 0 || len(bossArgs)%2 != 0 {
		s.ChannelMessageSend(m.ChannelID, "Invalid format. Expected: `!quests [player_name] <boss> <count> [boss] <count> ...` (supports comma or space separated)")
		return
	}

	// Parse boss/count pairs
	weekNumber, year := getCurrentWeek()
	updates := 0

	for i := 0; i < len(bossArgs); i += 2 {
		bossInput := strings.TrimSpace(bossArgs[i])
		bossName, ok := quests.ResolveBossName(bossInput)
		if !ok {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Invalid boss name: %s. Use full name, first letter, or key color. Valid bosses: %s", bossInput, strings.Join(quests.ValidBosses(), ", ")))
			continue
		}

		countStr := strings.TrimSpace(bossArgs[i+1])
		count, err := strconv.Atoi(countStr)
		if err != nil || count < 0 {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Invalid count: %s (must be a non-negative integer)", countStr))
			continue
		}

		err = h.db.UpsertQuest(ctx, m.Author.ID, playerName, weekNumber, year, bossName, count)
		if err != nil {
			l.Error("Failed to upsert quest", zap.Error(err))
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error updating quest for %s: %s", formatBossNameWithEmoji(bossName), err.Error()))
			continue
		}

		updates++
	}

	if updates > 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Updated %d quest(s) for **%s**", updates, playerName))
	}
}

func (h *questsHandler) handleComplete(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	l := ctxzap.Extract(ctx)

	if len(args) == 0 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!quests complete <boss> [amount]` - Marks a quest as complete (sets to 0) or reduces by amount")
		return
	}

	// Get player name
	playerName, err := h.db.GetPlayerName(ctx, m.Author.ID)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "No default player name registered. Use `!quests register <player_name>` first")
		return
	}

	// Resolve boss name from input (supports full name, single letter, or color)
	bossInput := strings.TrimSpace(args[0])
	bossName, ok := quests.ResolveBossName(bossInput)
	if !ok {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Invalid boss name: %s. Use full name, first letter, or key color.", bossInput))
		return
	}

	weekNumber, year := getCurrentWeek()

	// Get current quest to see what the current required_kills is
	questsList, err := h.db.GetPlayerQuests(ctx, playerName, weekNumber, year)
	if err != nil {
		l.Error("Failed to get player quests", zap.Error(err))
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting quests: %s", err.Error()))
		return
	}

	var currentRequiredKills int
	found := false
	for _, quest := range questsList {
		if quest.BossName == bossName {
			currentRequiredKills = quest.RequiredKills
			found = true
			break
		}
	}

	if !found {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No quest found for %s (%s) in week %d of %d", formatBossNameWithEmoji(bossName), bossInput, weekNumber, year))
		return
	}

	// Determine new required_kills value
	var newRequiredKills int
	var amount int
	if len(args) > 1 {
		// Amount provided - reduce by that amount
		amountStr := strings.TrimSpace(args[1])
		var err error
		amount, err = strconv.Atoi(amountStr)
		if err != nil || amount < 0 {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Invalid amount: %s (must be a non-negative integer)", amountStr))
			return
		}
		newRequiredKills = currentRequiredKills - amount
		if newRequiredKills < 0 {
			newRequiredKills = 0
		}
		l.Info("Completing quest with amount",
			zap.String("player", playerName),
			zap.String("boss", bossName),
			zap.Int("current", currentRequiredKills),
			zap.Int("amount", amount),
			zap.Int("new", newRequiredKills))
	} else {
		// No amount provided - set to 0 (complete)
		newRequiredKills = 0
		amount = currentRequiredKills
		l.Info("Completing quest (full)",
			zap.String("player", playerName),
			zap.String("boss", bossName),
			zap.Int("current", currentRequiredKills))
	}

	// Update the quest using UpdateQuestRequiredKills to preserve current_kills
	// (don't recalculate as if progress was made)
	err = h.db.UpdateQuestRequiredKills(ctx, m.Author.ID, playerName, weekNumber, year, bossName, newRequiredKills)
	if err != nil {
		l.Error("Failed to complete quest", zap.Error(err))
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error completing quest: %s", err.Error()))
		return
	}

	if newRequiredKills == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Marked %s (%s) as complete for **%s**", formatBossNameWithEmoji(bossName), bossInput, playerName))
	} else {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Reduced %s (%s) by %d for **%s** (now %d remaining)", formatBossNameWithEmoji(bossName), bossInput, amount, playerName, newRequiredKills))
	}
}

func (h *questsHandler) handleKeys(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	l := ctxzap.Extract(ctx)

	var playerName string
	var weekNumber, year int
	var err error

	// Parse arguments
	if len(args) == 0 {
		// Use default player and current week
		playerName, err = h.db.GetPlayerName(ctx, m.Author.ID)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, "No default player name registered. Use `!quests register <player_name>` first, or provide player name: `!quests keys <player_name> [week|date]`")
			return
		}
		weekNumber, year = getCurrentWeek()
	} else {
		// Check if first arg is a week/date or player name
		weekNum, yr, parseErr := parseWeekOrDate(args[0])
		if parseErr == nil {
			// First arg is week/date, use default player
			playerName, err = h.db.GetPlayerName(ctx, m.Author.ID)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "No default player name registered. Use `!quests register <player_name>` first")
				return
			}
			weekNumber, year = weekNum, yr
		} else {
			// First arg is player name
			playerName = args[0]
			if len(args) > 1 {
				weekNumber, year, err = parseWeekOrDate(args[1])
				if err != nil {
					s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Invalid week/date format: %s. Use ISO week number (1-53) or date (YYYY-MM-DD)", args[1]))
					return
				}
			} else {
				weekNumber, year = getCurrentWeek()
			}
		}
	}

	questsList, err := h.db.GetPlayerQuests(ctx, playerName, weekNumber, year)
	if err != nil {
		l.Error("Failed to get player quests", zap.Error(err))
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting quests: %s", err.Error()))
		return
	}

	if len(questsList) == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No quests found for **%s** in week %d of %d", playerName, weekNumber, year))
		return
	}

	// Filter quests with remaining kills and calculate key requirements
	type BossKeyReq struct {
		BossName      string
		KeyType       string
		RealKeys      int
		EstimatedKeys int
	}

	var bossReqs []BossKeyReq
	totalReal := 0
	totalEstimated := 0

	for _, quest := range questsList {
		keyType, ok := quests.GetKeyForBoss(quest.BossName)
		if !ok {
			continue
		}

		remainingKills := quest.RequiredKills - quest.CurrentKills
		if remainingKills <= 0 {
			continue
		}

		realKeys := remainingKills
		estimatedKeys := int(math.Round(float64(realKeys) * (1 - quests.YoinkBonus)))

		bossReqs = append(bossReqs, BossKeyReq{
			BossName:      quest.BossName,
			KeyType:       keyType,
			RealKeys:      realKeys,
			EstimatedKeys: estimatedKeys,
		})
		totalReal += realKeys
		totalEstimated += estimatedKeys
	}

	if len(bossReqs) == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No key requirements for **%s** in week %d of %d (all quests completed)", playerName, weekNumber, year))
		return
	}

	// Sort by boss name for consistent output
	sort.Slice(bossReqs, func(i, j int) bool {
		return bossReqs[i].BossName < bossReqs[j].BossName
	})

	// Build embed fields - group by key type for better organization
	keyGroups := make(map[string][]BossKeyReq)
	for _, req := range bossReqs {
		keyGroups[req.KeyType] = append(keyGroups[req.KeyType], req)
	}

	// Sort key types for consistent output
	keyTypes := make([]string, 0, len(keyGroups))
	for keyType := range keyGroups {
		keyTypes = append(keyTypes, keyType)
	}
	sort.Strings(keyTypes)

	fields := make([]*discordgo.MessageEmbedField, 0, len(keyTypes))
	for _, keyType := range keyTypes {
		var value strings.Builder
		for _, req := range keyGroups[keyType] {
			value.WriteString(fmt.Sprintf("%d (%d)\n", req.RealKeys, req.EstimatedKeys))
		}
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   formatBossNameWithEmoji(keyGroups[keyType][0].BossName),
			Value:  value.String(),
			Inline: true,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Key Requirements for %s", playerName),
		Description: fmt.Sprintf("Week %d, %d", weekNumber, year),
		Color:       0x3498db, // Blue color
		Fields:      fields,
	}

	s.ChannelMessageSendEmbeds(m.ChannelID, []*discordgo.MessageEmbed{embed})
}

func (h *questsHandler) handleClan(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	l := ctxzap.Extract(ctx)

	var weekNumber, year int
	var err error

	if len(args) > 0 {
		weekNumber, year, err = parseWeekOrDate(args[0])
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Invalid week/date format: %s. Use ISO week number (1-53) or date (YYYY-MM-DD)", args[0]))
			return
		}
	} else {
		weekNumber, year = getCurrentWeek()
	}

	questsList, err := h.db.GetAllQuestsForWeek(ctx, weekNumber, year)
	if err != nil {
		l.Error("Failed to get clan quests", zap.Error(err))
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting quests: %s", err.Error()))
		return
	}

	if len(questsList) == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No quests found for week %d of %d", weekNumber, year))
		return
	}

	// Log all quests for debugging
	l.Info("Clan quests retrieved", zap.Int("count", len(questsList)), zap.Int("week", weekNumber), zap.Int("year", year))
	for _, q := range questsList {
		l.Debug("Quest", zap.String("player", q.PlayerName), zap.String("boss", q.BossName), zap.Int("required", q.RequiredKills), zap.Int("current", q.CurrentKills))
	}

	// Aggregate by boss across all players - sum up all quests for each boss
	type BossKeyReq struct {
		BossName      string
		KeyType       string
		RealKeys      int
		EstimatedKeys int
	}

	bossReqsMap := make(map[string]*BossKeyReq)
	totalReal := 0
	totalEstimated := 0

	for _, quest := range questsList {
		keyType, ok := quests.GetKeyForBoss(quest.BossName)
		if !ok {
			continue
		}

		remainingKills := quest.RequiredKills - quest.CurrentKills
		if remainingKills <= 0 {
			continue
		}

		realKeys := remainingKills
		estimatedKeys := int(math.Round(float64(realKeys) * (1 - quests.YoinkBonus)))

		// Sum up all players' quests for this boss
		if req, exists := bossReqsMap[quest.BossName]; exists {
			// Already have this boss - add to existing totals
			req.RealKeys += realKeys
			req.EstimatedKeys += estimatedKeys
			l.Debug("Aggregating boss", zap.String("boss", quest.BossName), zap.String("player", quest.PlayerName), zap.Int("added", realKeys), zap.Int("total", req.RealKeys))
		} else {
			// First time seeing this boss - create new entry
			bossReqsMap[quest.BossName] = &BossKeyReq{
				BossName:      quest.BossName,
				KeyType:       keyType,
				RealKeys:      realKeys,
				EstimatedKeys: estimatedKeys,
			}
			l.Debug("New boss entry", zap.String("boss", quest.BossName), zap.String("player", quest.PlayerName), zap.Int("keys", realKeys))
		}
		totalReal += realKeys
		totalEstimated += estimatedKeys
	}

	if len(bossReqsMap) == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No key requirements for week %d of %d (all quests completed)", weekNumber, year))
		return
	}

	// Group by key type for better organization
	keyGroups := make(map[string][]*BossKeyReq)
	for _, req := range bossReqsMap {
		keyGroups[req.KeyType] = append(keyGroups[req.KeyType], req)
	}

	// Sort key types and bosses within each group for consistent output
	keyTypes := make([]string, 0, len(keyGroups))
	for keyType := range keyGroups {
		keyTypes = append(keyTypes, keyType)
		sort.Slice(keyGroups[keyType], func(i, j int) bool {
			return keyGroups[keyType][i].BossName < keyGroups[keyType][j].BossName
		})
	}
	sort.Strings(keyTypes)

	fields := make([]*discordgo.MessageEmbedField, 0, len(keyTypes))
	for _, keyType := range keyTypes {
		var value strings.Builder
		for _, req := range keyGroups[keyType] {
			value.WriteString(fmt.Sprintf("%d (%d)\n", req.RealKeys, req.EstimatedKeys))
		}
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   formatBossNameWithEmoji(keyGroups[keyType][0].BossName),
			Value:  value.String(),
			Inline: true,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Clan Key Requirements",
		Description: fmt.Sprintf("Week %d, %d", weekNumber, year),
		Color:       0x2ecc71, // Green color
		Fields:      fields,
	}

	s.ChannelMessageSendEmbeds(m.ChannelID, []*discordgo.MessageEmbed{embed})
}

func (h *questsHandler) handlePing(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate) {
	l := ctxzap.Extract(ctx)

	// Get the user's default player name
	playerName, err := h.db.GetPlayerName(ctx, m.Author.ID)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "No default player name registered. Use `!quests register <player_name>` first")
		return
	}

	weekNumber, year := getCurrentWeek()

	// Get players with matching quests
	matchingPlayers, err := h.db.GetPlayersWithMatchingQuests(ctx, playerName, weekNumber, year)
	if err != nil {
		l.Error("Failed to get matching players", zap.Error(err))
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error finding matching players: %s", err.Error()))
		return
	}

	if len(matchingPlayers) == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No other players found with matching quests for **%s** in week %d of %d", playerName, weekNumber, year))
		return
	}

	// Build ping list (unique Discord user IDs)
	pingedUsers := make(map[string]bool)
	var pingMentions []string

	// Aggregate matching players' quests by boss
	type BossKeyReq struct {
		BossName      string
		KeyType       string
		RealKeys      int
		EstimatedKeys int
	}

	bossReqsMap := make(map[string]*BossKeyReq)

	for _, pqi := range matchingPlayers {
		if !pingedUsers[pqi.DiscordUserID] {
			pingedUsers[pqi.DiscordUserID] = true
			pingMentions = append(pingMentions, fmt.Sprintf("<@%s>", pqi.DiscordUserID))
		}

		// Calculate key requirements for this quest
		keyType, ok := quests.GetKeyForBoss(pqi.BossName)
		if !ok {
			continue
		}

		remainingKills := pqi.RequiredKills - pqi.CurrentKills
		if remainingKills <= 0 {
			continue
		}

		realKeys := remainingKills
		estimatedKeys := int(math.Round(float64(realKeys) * (1 - quests.YoinkBonus)))

		// Sum up all matching players' quests for this boss
		if req, exists := bossReqsMap[pqi.BossName]; exists {
			req.RealKeys += realKeys
			req.EstimatedKeys += estimatedKeys
		} else {
			bossReqsMap[pqi.BossName] = &BossKeyReq{
				BossName:      pqi.BossName,
				KeyType:       keyType,
				RealKeys:      realKeys,
				EstimatedKeys: estimatedKeys,
			}
		}
	}

	if len(bossReqsMap) == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s - You have matching quests, but all are completed!", strings.Join(pingMentions, " ")))
		return
	}

	// Group by key type for better organization
	keyGroups := make(map[string][]*BossKeyReq)
	for _, req := range bossReqsMap {
		keyGroups[req.KeyType] = append(keyGroups[req.KeyType], req)
	}

	// Sort key types and bosses within each group for consistent output
	keyTypes := make([]string, 0, len(keyGroups))
	for keyType := range keyGroups {
		keyTypes = append(keyTypes, keyType)
		sort.Slice(keyGroups[keyType], func(i, j int) bool {
			return keyGroups[keyType][i].BossName < keyGroups[keyType][j].BossName
		})
	}
	sort.Strings(keyTypes)

	fields := make([]*discordgo.MessageEmbedField, 0, len(keyTypes))
	for _, keyType := range keyTypes {
		var value strings.Builder
		for _, req := range keyGroups[keyType] {
			value.WriteString(fmt.Sprintf("%d (%d)\n", req.RealKeys, req.EstimatedKeys))
		}
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   formatBossNameWithEmoji(keyGroups[keyType][0].BossName),
			Value:  value.String(),
			Inline: true,
		})
	}

	// Send ping message first
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s - You have matching quests!", strings.Join(pingMentions, " ")))

	// Send embed with quest details
	embed := &discordgo.MessageEmbed{
		Title:       "Matching Quest Requirements",
		Description: fmt.Sprintf("Week %d, %d", weekNumber, year),
		Color:       0xe67e22, // Orange color
		Fields:      fields,
	}

	s.ChannelMessageSendEmbeds(m.ChannelID, []*discordgo.MessageEmbed{embed})
}

func (h *questsHandler) handleBossPing(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, bossName string) {
	l := ctxzap.Extract(ctx)

	weekNumber, year := getCurrentWeek()

	// Get all players with a quest for this boss
	players, err := h.db.GetPlayersWithBossQuest(ctx, bossName, weekNumber, year)
	if err != nil {
		l.Error("Failed to get players with boss quest", zap.Error(err))
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error finding players: %s", err.Error()))
		return
	}

	if len(players) == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No players found with %s quests in week %d of %d", formatBossNameWithEmoji(bossName), weekNumber, year))
		return
	}

	// Build ping list (unique Discord user IDs)
	pingedUsers := make(map[string]bool)
	var pingMentions []string
	var questInfo []string

	for _, pqi := range players {
		if !pingedUsers[pqi.DiscordUserID] {
			pingedUsers[pqi.DiscordUserID] = true
			pingMentions = append(pingMentions, fmt.Sprintf("<@%s>", pqi.DiscordUserID))
		}

		remaining := pqi.RequiredKills - pqi.CurrentKills
		questInfo = append(questInfo, fmt.Sprintf("**%s**: %d/%d remaining", pqi.PlayerName, remaining, pqi.RequiredKills))
	}

	// Send ping message
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s - You have %s quests!", strings.Join(pingMentions, " "), formatBossNameWithEmoji(bossName)))

	// Send embed with quest details
	keyType, _ := quests.GetKeyForBoss(bossName)
	description := fmt.Sprintf("Week %d, %d", weekNumber, year)
	if keyType != "" {
		description += fmt.Sprintf(" • Key: %s", cases.Title(language.English).String(keyType))
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s Quest Details", formatBossNameWithEmoji(bossName)),
		Description: description,
		Color:       0xe67e22, // Orange color
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Players with Quest",
				Value:  strings.Join(questInfo, "\n"),
				Inline: false,
			},
		},
	}

	s.ChannelMessageSendEmbeds(m.ChannelID, []*discordgo.MessageEmbed{embed})
}

func (h *questsHandler) handleWho(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	l := ctxzap.Extract(ctx)

	var weekNumber, year int
	var err error

	if len(args) > 0 {
		weekNumber, year, err = parseWeekOrDate(args[0])
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Invalid week/date format: %s. Use ISO week number (1-53) or date (YYYY-MM-DD)", args[0]))
			return
		}
	} else {
		weekNumber, year = getCurrentWeek()
	}

	questsList, err := h.db.GetAllQuestsForWeek(ctx, weekNumber, year)
	if err != nil {
		l.Error("Failed to get quests", zap.Error(err))
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting quests: %s", err.Error()))
		return
	}

	if len(questsList) == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No quests found for week %d of %d", weekNumber, year))
		return
	}

	// Group by boss, then by player
	type PlayerKeyInfo struct {
		PlayerName string
		KeyCount   int
	}

	bossPlayers := make(map[string][]PlayerKeyInfo)

	for _, quest := range questsList {
		remainingKills := quest.RequiredKills - quest.CurrentKills
		if remainingKills <= 0 {
			continue
		}

		// Keys = remaining kills
		keyCount := remainingKills

		bossPlayers[quest.BossName] = append(bossPlayers[quest.BossName], PlayerKeyInfo{
			PlayerName: quest.PlayerName,
			KeyCount:   keyCount,
		})
	}

	if len(bossPlayers) == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No key requirements for week %d of %d (all quests completed)", weekNumber, year))
		return
	}

	// Sort bosses alphabetically
	bossNames := make([]string, 0, len(bossPlayers))
	for bossName := range bossPlayers {
		bossNames = append(bossNames, bossName)
	}
	sort.Strings(bossNames)

	// Build embed fields - one field per boss
	fields := make([]*discordgo.MessageEmbedField, 0, len(bossNames))
	for _, bossName := range bossNames {
		players := bossPlayers[bossName]
		
		// Sort players by key count (descending), then by name
		sort.Slice(players, func(i, j int) bool {
			if players[i].KeyCount != players[j].KeyCount {
				return players[i].KeyCount > players[j].KeyCount
			}
			return players[i].PlayerName < players[j].PlayerName
		})

		var value strings.Builder
		for _, player := range players {
			value.WriteString(fmt.Sprintf("%s: %d\n", player.PlayerName, player.KeyCount))
		}

		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   formatBossNameWithEmoji(bossName),
			Value:  value.String(),
			Inline: true,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Who Has Keys",
		Description: fmt.Sprintf("Week %d, %d", weekNumber, year),
		Color:       0x9b59b6, // Purple color (same as help)
		Fields:      fields,
	}

	s.ChannelMessageSendEmbeds(m.ChannelID, []*discordgo.MessageEmbed{embed})
}

// Helper functions

// formatBossNameWithEmoji formats a boss name with its corresponding key color emoji
func formatBossNameWithEmoji(bossName string) string {
	// Map key colors to emoji/symbols for visual distinction
	colorEmoji := map[string]string{
		"brown":        "🟤",
		"gray":         "⚫",
		"blue":         "🔵",
		"gold":         "⭐",
		"red":          "🔴",
		"green":        "🟢",
		"otherworldly": "💫",
		"ancient":      "🏛️",
		"book":         "📖",
	}

	keyType, ok := quests.GetKeyForBoss(bossName)
	if !ok {
		return cases.Title(language.English).String(bossName)
	}

	keyColor, hasColor := quests.KeyToColor[keyType]
	fieldName := cases.Title(language.English).String(bossName)
	if hasColor {
		emoji := colorEmoji[strings.ToLower(keyColor)]
		if emoji != "" {
			fieldName = fmt.Sprintf("%s %s", emoji, fieldName)
		}
	}

	return fieldName
}

func getCurrentWeek() (weekNumber, year int) {
	now := time.Now().UTC()
	year, weekNumber = now.ISOWeek()
	return weekNumber, year
}

func parseWeekOrDate(input string) (weekNumber, year int, err error) {
	// Try parsing as date first (YYYY-MM-DD)
	if t, err := time.Parse("2006-01-02", input); err == nil {
		year, weekNumber = t.ISOWeek()
		return weekNumber, year, nil
	}

	// Try parsing as week number (assume current year)
	if weekNum, err := strconv.Atoi(input); err == nil {
		if weekNum < 1 || weekNum > 53 {
			return 0, 0, fmt.Errorf("week number must be between 1 and 53")
		}
		year, _ = time.Now().UTC().ISOWeek()
		return weekNum, year, nil
	}

	return 0, 0, fmt.Errorf("invalid format")
}
