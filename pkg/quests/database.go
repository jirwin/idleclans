package quests

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

type DB struct {
	db *sqlx.DB
}

func NewDB(dbPath string) (*DB, error) {
	db, err := sqlx.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	d := &DB{db: db}
	if err := d.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return d, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS players (
		discord_user_id TEXT PRIMARY KEY,
		player_name TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS weekly_quests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		discord_user_id TEXT NOT NULL,
		player_name TEXT NOT NULL,
		week_number INTEGER NOT NULL,
		year INTEGER NOT NULL,
		boss_name TEXT NOT NULL,
		required_kills INTEGER NOT NULL,
		max_required_kills INTEGER NOT NULL,
		current_kills INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(discord_user_id, week_number, year, boss_name)
	);

	CREATE TABLE IF NOT EXISTS quest_kills (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		quest_id INTEGER NOT NULL,
		kills_completed INTEGER NOT NULL,
		recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(quest_id) REFERENCES weekly_quests(id)
	);

	CREATE INDEX IF NOT EXISTS idx_weekly_quests_user_week ON weekly_quests(discord_user_id, week_number, year);
	CREATE INDEX IF NOT EXISTS idx_weekly_quests_player_week ON weekly_quests(player_name, week_number, year);
	CREATE INDEX IF NOT EXISTS idx_quest_kills_quest_id ON quest_kills(quest_id);
	
	CREATE TABLE IF NOT EXISTS player_keys (
		player_name TEXT NOT NULL,
		key_type TEXT NOT NULL,
		count INTEGER NOT NULL DEFAULT 0,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (player_name, key_type)
	);

	CREATE TABLE IF NOT EXISTS player_alts (
		discord_user_id TEXT NOT NULL,
		player_name TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (discord_user_id, player_name)
	);

	CREATE INDEX IF NOT EXISTS idx_player_alts_user ON player_alts(discord_user_id);

	CREATE TABLE IF NOT EXISTS web_sessions (
		session_id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		username TEXT NOT NULL,
		avatar TEXT,
		expires_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_web_sessions_user ON web_sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_web_sessions_expires ON web_sessions(expires_at);
	`

	_, err := d.db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: Add max_required_kills column if it doesn't exist
	// This handles existing databases that were created before this column was added
	// SQLite's ALTER TABLE ADD COLUMN will fail if column exists, so we ignore that error
	_, _ = d.db.Exec(`ALTER TABLE weekly_quests ADD COLUMN max_required_kills INTEGER`)

	// Update existing rows to set max_required_kills = required_kills if it's NULL
	// This handles both new columns (which will be NULL) and ensures consistency
	_, _ = d.db.Exec(`UPDATE weekly_quests SET max_required_kills = required_kills WHERE max_required_kills IS NULL`)

	// Migration: Convert player_keys from discord_user_id to player_name
	// Check if old schema exists (has discord_user_id column)
	var hasOldSchema bool
	row := d.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('player_keys') WHERE name='discord_user_id'`)
	var count int
	if err := row.Scan(&count); err == nil && count > 0 {
		hasOldSchema = true
	}

	if hasOldSchema {
		// Migrate old data: copy keys from discord_user_id to player_name
		_, _ = d.db.Exec(`
			CREATE TABLE IF NOT EXISTS player_keys_new (
				player_name TEXT NOT NULL,
				key_type TEXT NOT NULL,
				count INTEGER NOT NULL DEFAULT 0,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (player_name, key_type)
			)
		`)
		// Copy data from old table, joining with players to get player_name
		_, _ = d.db.Exec(`
			INSERT OR IGNORE INTO player_keys_new (player_name, key_type, count, updated_at)
			SELECT p.player_name, pk.key_type, pk.count, pk.updated_at
			FROM player_keys pk
			JOIN players p ON pk.discord_user_id = p.discord_user_id
		`)
		// Drop old table and rename new one
		_, _ = d.db.Exec(`DROP TABLE player_keys`)
		_, _ = d.db.Exec(`ALTER TABLE player_keys_new RENAME TO player_keys`)
	}

	return nil
}

// RegisterPlayer registers or updates a default player name for a Discord user
func (d *DB) RegisterPlayer(ctx context.Context, discordUserID, playerName string) error {
	l := ctxzap.Extract(ctx)
	l.Info("Registering player", zap.String("discord_user_id", discordUserID), zap.String("player_name", playerName))

	query := `
		INSERT INTO players (discord_user_id, player_name, created_at, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(discord_user_id) DO UPDATE SET
			player_name = excluded.player_name,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err := d.db.ExecContext(ctx, query, discordUserID, playerName)
	return err
}

// GetPlayerName returns the registered player name for a Discord user
func (d *DB) GetPlayerName(ctx context.Context, discordUserID string) (string, error) {
	var playerName string
	query := `SELECT player_name FROM players WHERE discord_user_id = ?`
	err := d.db.GetContext(ctx, &playerName, query, discordUserID)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no default player name registered")
	}
	if err != nil {
		return "", err
	}
	return playerName, nil
}

// UnregisterPlayer removes the player registration for a Discord user (keeps other data)
func (d *DB) UnregisterPlayer(ctx context.Context, discordUserID string) error {
	l := ctxzap.Extract(ctx)
	l.Info("Unregistering player", zap.String("discord_user_id", discordUserID))

	query := `DELETE FROM players WHERE discord_user_id = ?`
	_, err := d.db.ExecContext(ctx, query, discordUserID)
	return err
}

// DeletePlayer removes all data for a Discord user (player, alts, keys, quests)
func (d *DB) DeletePlayer(ctx context.Context, discordUserID string) error {
	l := ctxzap.Extract(ctx)
	l.Info("Deleting player and all associated data", zap.String("discord_user_id", discordUserID))

	// Get player name first to clean up player_keys
	playerName, _ := d.GetPlayerName(ctx, discordUserID)
	alts, _ := d.GetAlts(ctx, discordUserID)

	// Delete in order to respect any foreign key constraints
	// Delete player keys for main and alts
	if playerName != "" {
		_, _ = d.db.ExecContext(ctx, `DELETE FROM player_keys WHERE player_name = ?`, playerName)
	}
	for _, alt := range alts {
		_, _ = d.db.ExecContext(ctx, `DELETE FROM player_keys WHERE player_name = ?`, alt)
	}

	// Delete quests
	_, _ = d.db.ExecContext(ctx, `DELETE FROM weekly_quests WHERE discord_user_id = ?`, discordUserID)

	// Delete quest kills
	_, _ = d.db.ExecContext(ctx, `DELETE FROM quest_kills WHERE discord_user_id = ?`, discordUserID)

	// Delete alts
	_, _ = d.db.ExecContext(ctx, `DELETE FROM player_alts WHERE discord_user_id = ?`, discordUserID)

	// Delete player registration
	_, err := d.db.ExecContext(ctx, `DELETE FROM players WHERE discord_user_id = ?`, discordUserID)

	// Delete sessions
	_, _ = d.db.ExecContext(ctx, `DELETE FROM web_sessions WHERE user_id = ?`, discordUserID)

	return err
}

// WeeklyQuestRow represents a row in the weekly_quests table
type WeeklyQuestRow struct {
	ID               int    `db:"id"`
	DiscordUserID    string `db:"discord_user_id"`
	PlayerName       string `db:"player_name"`
	WeekNumber       int    `db:"week_number"`
	Year             int    `db:"year"`
	BossName         string `db:"boss_name"`
	RequiredKills    int    `db:"required_kills"`
	MaxRequiredKills int    `db:"max_required_kills"`
	CurrentKills     int    `db:"current_kills"`
}

// UpsertQuest updates or inserts a quest for a player
func (d *DB) UpsertQuest(ctx context.Context, discordUserID, playerName string, weekNumber, year int, bossName string, requiredKills int) error {
	l := ctxzap.Extract(ctx)

	// First, get the existing quest if it exists
	var existing WeeklyQuestRow
	query := `SELECT id, required_kills, max_required_kills, current_kills FROM weekly_quests 
		WHERE discord_user_id = ? AND week_number = ? AND year = ? AND boss_name = ?`
	err := d.db.GetContext(ctx, &existing, query, discordUserID, weekNumber, year, bossName)

	if err == sql.ErrNoRows {
		// Insert new quest - max_required_kills starts as required_kills
		insertQuery := `
			INSERT INTO weekly_quests (discord_user_id, player_name, week_number, year, boss_name, required_kills, max_required_kills, current_kills, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`
		_, err = d.db.ExecContext(ctx, insertQuery, discordUserID, playerName, weekNumber, year, bossName, requiredKills, requiredKills)
		if err != nil {
			return err
		}
		l.Info("Created new quest", zap.String("player", playerName), zap.String("boss", bossName), zap.Int("kills", requiredKills))
	} else if err != nil {
		return err
	} else {
		// Update existing quest
		newMaxRequired := existing.MaxRequiredKills

		// If quest was zeroed out (existing.RequiredKills == 0) and now being set to a positive value,
		// treat it as a fresh quest and reset max_required_kills
		if existing.RequiredKills == 0 && requiredKills > 0 {
			newMaxRequired = requiredKills
		} else if requiredKills > existing.MaxRequiredKills {
			// If required_kills increased, update max_required_kills
			newMaxRequired = requiredKills
		}

		// Calculate new current_kills
		// current_kills represents actual kills completed (tracked via deltas)
		// When required_kills decreases naturally (user updates quest), we calculate progress
		// When required_kills increases, we preserve current_kills
		var newCurrentKills int
		if requiredKills == 0 {
			// Zeroing out - reset current_kills
			newCurrentKills = 0
		} else if existing.RequiredKills == 0 && requiredKills > 0 {
			// Quest was zeroed and now being set to positive - treat as fresh quest
			newCurrentKills = 0
		} else if requiredKills < existing.RequiredKills {
			// required_kills decreased - this could be:
			// 1. Natural progress (user updating quest as they complete kills)
			// 2. Manual reduction via complete command
			// We calculate based on max to track progress, but this might over-count
			// if it's a manual reduction. However, the delta tracking will handle this.
			oldCalculatedKills := existing.MaxRequiredKills - existing.RequiredKills
			if oldCalculatedKills < 0 {
				oldCalculatedKills = 0
			}
			newCalculatedKills := newMaxRequired - requiredKills
			if newCalculatedKills < 0 {
				newCalculatedKills = 0
			}
			// Use the calculated value, but ensure we don't go below existing current_kills
			// (preserve any manually tracked progress)
			newCurrentKills = newCalculatedKills
			if newCurrentKills < existing.CurrentKills {
				newCurrentKills = existing.CurrentKills
			}
		} else {
			// required_kills increased or stayed same - preserve existing current_kills
			// Don't lose progress when quest requirements increase
			newCurrentKills = existing.CurrentKills
		}

		// Track delta if kills were completed (current_kills increased)
		// Only record positive deltas - when required_kills decreases
		oldCurrentKills := existing.MaxRequiredKills - existing.RequiredKills
		if oldCurrentKills < 0 {
			oldCurrentKills = 0
		}
		delta := newCurrentKills - oldCurrentKills

		if delta > 0 {
			// Record the delta in quest_kills table (only when kills are actually completed)
			killQuery := `INSERT INTO quest_kills (quest_id, kills_completed, recorded_at) VALUES (?, ?, CURRENT_TIMESTAMP)`
			_, err = d.db.ExecContext(ctx, killQuery, existing.ID, delta)
			if err != nil {
				l.Error("Failed to record kill delta", zap.Error(err))
			}
		}
		// If delta is negative (required_kills increased), we don't record it
		// The current_kills will be recalculated correctly based on max_required_kills

		updateQuery := `
			UPDATE weekly_quests 
			SET required_kills = ?, max_required_kills = ?, current_kills = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`
		_, err = d.db.ExecContext(ctx, updateQuery, requiredKills, newMaxRequired, newCurrentKills, existing.ID)
		if err != nil {
			return err
		}
		l.Info("Updated quest", zap.String("player", playerName), zap.String("boss", bossName), zap.Int("kills", requiredKills), zap.Int("current", newCurrentKills))
	}

	return nil
}

// UpdateQuestRequiredKills updates only the required_kills without recalculating current_kills
// This is used by the complete command to manually reduce requirements
func (d *DB) UpdateQuestRequiredKills(ctx context.Context, discordUserID, playerName string, weekNumber, year int, bossName string, requiredKills int) error {
	l := ctxzap.Extract(ctx)

	// Get the existing quest
	var existing WeeklyQuestRow
	query := `SELECT id, required_kills, max_required_kills, current_kills FROM weekly_quests 
		WHERE discord_user_id = ? AND week_number = ? AND year = ? AND boss_name = ?`
	err := d.db.GetContext(ctx, &existing, query, discordUserID, weekNumber, year, bossName)

	if err == sql.ErrNoRows {
		// Quest doesn't exist - create it normally
		return d.UpsertQuest(ctx, discordUserID, playerName, weekNumber, year, bossName, requiredKills)
	} else if err != nil {
		return err
	}

	// Update max_required_kills if needed
	newMaxRequired := existing.MaxRequiredKills
	if requiredKills == 0 {
		// Zeroing out - reset current_kills
		existing.CurrentKills = 0
	} else if requiredKills > existing.MaxRequiredKills {
		// If required_kills increased, update max_required_kills
		newMaxRequired = requiredKills
	} else if existing.RequiredKills == 0 && requiredKills > 0 {
		// Quest was zeroed and now being set to positive - reset max
		newMaxRequired = requiredKills
		existing.CurrentKills = 0
	}
	// Otherwise, preserve existing current_kills (don't recalculate)

	updateQuery := `
		UPDATE weekly_quests 
		SET required_kills = ?, max_required_kills = ?, current_kills = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`
	_, err = d.db.ExecContext(ctx, updateQuery, requiredKills, newMaxRequired, existing.CurrentKills, existing.ID)
	if err != nil {
		return err
	}
	l.Info("Updated quest required kills", zap.String("player", playerName), zap.String("boss", bossName), zap.Int("kills", requiredKills), zap.Int("current", existing.CurrentKills))
	return nil
}

// Quest represents a weekly quest entry
type Quest struct {
	PlayerName    string `db:"player_name"`
	BossName      string `db:"boss_name"`
	RequiredKills int    `db:"required_kills"`
	CurrentKills  int    `db:"current_kills"`
}

// GetPlayerQuests returns all quests for a player in a specific week
func (d *DB) GetPlayerQuests(ctx context.Context, playerName string, weekNumber, year int) ([]Quest, error) {
	query := `
		SELECT player_name, boss_name, required_kills, current_kills
		FROM weekly_quests
		WHERE player_name = ? AND week_number = ? AND year = ?
		ORDER BY boss_name
	`
	var quests []Quest
	err := d.db.SelectContext(ctx, &quests, query, playerName, weekNumber, year)
	return quests, err
}

// GetPlayerQuestsByDiscordID returns all quests for a Discord user in a specific week
func (d *DB) GetPlayerQuestsByDiscordID(ctx context.Context, discordUserID string, weekNumber, year int) ([]Quest, error) {
	query := `
		SELECT player_name, boss_name, required_kills, current_kills
		FROM weekly_quests
		WHERE discord_user_id = ? AND week_number = ? AND year = ?
		ORDER BY boss_name
	`
	var quests []Quest
	err := d.db.SelectContext(ctx, &quests, query, discordUserID, weekNumber, year)
	return quests, err
}

// GetAllQuestsForWeek returns all quests for all players in a specific week
func (d *DB) GetAllQuestsForWeek(ctx context.Context, weekNumber, year int) ([]Quest, error) {
	query := `
		SELECT player_name, boss_name, required_kills, current_kills
		FROM weekly_quests
		WHERE week_number = ? AND year = ?
		ORDER BY player_name, boss_name
	`
	var quests []Quest
	err := d.db.SelectContext(ctx, &quests, query, weekNumber, year)
	return quests, err
}

// PlayerQuestInfo contains quest info with Discord user ID
type PlayerQuestInfo struct {
	DiscordUserID string `db:"discord_user_id"`
	PlayerName    string `db:"player_name"`
	BossName      string `db:"boss_name"`
	RequiredKills int    `db:"required_kills"`
	CurrentKills  int    `db:"current_kills"`
}

// GetPlayersWithMatchingQuests returns all players who have the same boss quests as the given player
// A player matches if they have at least one of the same bosses
// It also considers alts - the Discord user who owns each player (as main or alt) is returned
func (d *DB) GetPlayersWithMatchingQuests(ctx context.Context, playerName string, weekNumber, year int) ([]PlayerQuestInfo, error) {
	// First, get the boss names for the given player
	playerBossesQuery := `
		SELECT DISTINCT boss_name
		FROM weekly_quests
		WHERE player_name = ? AND week_number = ? AND year = ?
	`
	type bossNameRow struct {
		BossName string `db:"boss_name"`
	}
	var bossRows []bossNameRow
	err := d.db.SelectContext(ctx, &bossRows, playerBossesQuery, playerName, weekNumber, year)
	if err != nil {
		return nil, err
	}

	bossNames := make([]string, len(bossRows))
	for i, row := range bossRows {
		bossNames[i] = row.BossName
	}

	if len(bossNames) == 0 {
		return []PlayerQuestInfo{}, nil
	}

	// Build query to find all players with matching bosses
	// Find players who have at least one of the same bosses
	placeholders := ""
	for i := 0; i < len(bossNames); i++ {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}

	// Find players who have at least one of the same bosses
	query := fmt.Sprintf(`
		SELECT wq.discord_user_id, wq.player_name, wq.boss_name, wq.required_kills, wq.current_kills
		FROM weekly_quests wq
		WHERE wq.week_number = ? AND wq.year = ?
		AND wq.boss_name IN (%s)
		AND wq.player_name != ?
		ORDER BY wq.player_name, wq.boss_name
	`, placeholders)

	args := make([]interface{}, 0, 3+len(bossNames))
	args = append(args, weekNumber, year)
	for _, boss := range bossNames {
		args = append(args, boss)
	}
	args = append(args, playerName)

	var results []PlayerQuestInfo
	err = d.db.SelectContext(ctx, &results, query, args...)
	if err != nil {
		return nil, err
	}

	// For each result, check if the player_name is owned by someone as main or alt
	for i := range results {
		pName := results[i].PlayerName

		// Check if this player is someone's main
		var ownerID string
		mainQuery := `SELECT discord_user_id FROM players WHERE player_name = ?`
		err := d.db.GetContext(ctx, &ownerID, mainQuery, pName)
		if err == nil && ownerID != results[i].DiscordUserID {
			results[i].DiscordUserID = ownerID
			continue
		}

		// Check if this player is someone's alt
		altQuery := `SELECT discord_user_id FROM player_alts WHERE player_name = ?`
		err = d.db.GetContext(ctx, &altQuery, altQuery, pName)
		if err == nil && ownerID != results[i].DiscordUserID {
			results[i].DiscordUserID = ownerID
		}
	}

	return results, nil
}

// GetPlayersWithBossQuest returns all players who have a quest for the specified boss
// It also finds Discord users who own each player (as main or alt) so they can be pinged
func (d *DB) GetPlayersWithBossQuest(ctx context.Context, bossName string, weekNumber, year int) ([]PlayerQuestInfo, error) {
	// Get quests for this boss
	query := `
		SELECT wq.discord_user_id, wq.player_name, wq.boss_name, wq.required_kills, wq.current_kills
		FROM weekly_quests wq
		WHERE wq.week_number = ? AND wq.year = ? AND wq.boss_name = ?
		AND (wq.required_kills - wq.current_kills) > 0
		ORDER BY wq.player_name
	`

	var results []PlayerQuestInfo
	err := d.db.SelectContext(ctx, &results, query, weekNumber, year, bossName)
	if err != nil {
		return nil, err
	}

	// For each result, also check if the player_name is owned by someone else as main or alt
	// This ensures the owner gets pinged even if they didn't submit the quest
	for i := range results {
		playerName := results[i].PlayerName
		
		// Check if this player is someone's main
		var ownerID string
		mainQuery := `SELECT discord_user_id FROM players WHERE player_name = ?`
		err := d.db.GetContext(ctx, &ownerID, mainQuery, playerName)
		if err == nil && ownerID != results[i].DiscordUserID {
			// Player is owned by someone else - update to their discord_user_id
			results[i].DiscordUserID = ownerID
			continue
		}
		
		// Check if this player is someone's alt
		altQuery := `SELECT discord_user_id FROM player_alts WHERE player_name = ?`
		err = d.db.GetContext(ctx, &ownerID, altQuery, playerName)
		if err == nil && ownerID != results[i].DiscordUserID {
			// Player is an alt owned by someone else - update to their discord_user_id
			results[i].DiscordUserID = ownerID
		}
	}

	return results, nil
}

// UpsertPlayerKeys updates the key count for a specific key type for a player (by player name)
func (d *DB) UpsertPlayerKeys(ctx context.Context, playerName string, keyType string, count int) error {
	l := ctxzap.Extract(ctx)
	
	query := `
		INSERT INTO player_keys (player_name, key_type, count, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(player_name, key_type) DO UPDATE SET
			count = excluded.count,
			updated_at = CURRENT_TIMESTAMP
	`
	
	_, err := d.db.ExecContext(ctx, query, playerName, keyType, count)
	if err != nil {
		l.Error("Failed to upsert player keys", zap.Error(err), zap.String("player_name", playerName), zap.String("key", keyType), zap.Int("count", count))
		return err
	}
	
	return nil
}

// GetPlayerKeys returns all key counts for a player (by player name)
func (d *DB) GetPlayerKeys(ctx context.Context, playerName string) (map[string]int, error) {
	query := `SELECT key_type, count FROM player_keys WHERE player_name = ?`
	
	type keyRow struct {
		KeyType string `db:"key_type"`
		Count   int    `db:"count"`
	}
	
	var rows []keyRow
	err := d.db.SelectContext(ctx, &rows, query, playerName)
	if err != nil {
		return nil, err
	}
	
	result := make(map[string]int)
	for _, row := range rows {
		result[row.KeyType] = row.Count
	}
	
	return result, nil
}

// GetPlayerKeyCount returns the count for a specific key type (by player name)
func (d *DB) GetPlayerKeyCount(ctx context.Context, playerName string, keyType string) (int, error) {
	var count int
	query := `SELECT count FROM player_keys WHERE player_name = ? AND key_type = ?`
	
	err := d.db.GetContext(ctx, &count, query, playerName, keyType)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	
	return count, nil
}

// PlayerKeyEntry represents a key entry with player name
type PlayerKeyEntry struct {
	PlayerName string `db:"player_name"`
	KeyType    string `db:"key_type"`
	Count      int    `db:"count"`
}

// GetAllPlayerKeys returns all keys for all players
func (d *DB) GetAllPlayerKeys(ctx context.Context) ([]PlayerKeyEntry, error) {
	query := `
		SELECT player_name, key_type, count
		FROM player_keys
		WHERE count > 0
		ORDER BY key_type, player_name
	`
	var results []PlayerKeyEntry
	err := d.db.SelectContext(ctx, &results, query)
	return results, err
}

// RegisterAlt adds an alternate player name for a Discord user
func (d *DB) RegisterAlt(ctx context.Context, discordUserID, playerName string) error {
	l := ctxzap.Extract(ctx)
	l.Info("Registering alt", zap.String("discord_user_id", discordUserID), zap.String("player_name", playerName))

	query := `
		INSERT INTO player_alts (discord_user_id, player_name, created_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(discord_user_id, player_name) DO NOTHING
	`
	_, err := d.db.ExecContext(ctx, query, discordUserID, playerName)
	return err
}

// RemoveAlt removes an alternate player name for a Discord user
func (d *DB) RemoveAlt(ctx context.Context, discordUserID, playerName string) error {
	l := ctxzap.Extract(ctx)
	l.Info("Removing alt", zap.String("discord_user_id", discordUserID), zap.String("player_name", playerName))

	query := `DELETE FROM player_alts WHERE discord_user_id = ? AND player_name = ?`
	_, err := d.db.ExecContext(ctx, query, discordUserID, playerName)
	return err
}

// GetAlts returns all alternate player names for a Discord user
func (d *DB) GetAlts(ctx context.Context, discordUserID string) ([]string, error) {
	query := `SELECT player_name FROM player_alts WHERE discord_user_id = ? ORDER BY player_name`
	
	var alts []string
	err := d.db.SelectContext(ctx, &alts, query, discordUserID)
	if err != nil {
		return nil, err
	}
	
	return alts, nil
}

// GetAllPlayerNames returns all player names for a Discord user (main + alts)
func (d *DB) GetAllPlayerNames(ctx context.Context, discordUserID string) ([]string, error) {
	var names []string
	
	// Get main player name
	mainName, err := d.GetPlayerName(ctx, discordUserID)
	if err == nil && mainName != "" {
		names = append(names, mainName)
	}
	
	// Get alts
	alts, err := d.GetAlts(ctx, discordUserID)
	if err != nil {
		return names, err
	}
	
	names = append(names, alts...)
	return names, nil
}

// PlayerRow represents a row in the players table
type PlayerRow struct {
	DiscordUserID string `db:"discord_user_id"`
	PlayerName    string `db:"player_name"`
}

// GetAllPlayers returns all registered players
func (d *DB) GetAllPlayers(ctx context.Context) ([]PlayerRow, error) {
	query := `SELECT discord_user_id, player_name FROM players ORDER BY player_name`
	var players []PlayerRow
	err := d.db.SelectContext(ctx, &players, query)
	if err != nil {
		return nil, err
	}
	return players, nil
}

// GetAllRegisteredPlayerNames returns all registered player names (main characters and alts)
func (d *DB) GetAllRegisteredPlayerNames(ctx context.Context) ([]string, error) {
	// Get main characters
	mainQuery := `SELECT player_name FROM players ORDER BY player_name`
	var mainPlayers []string
	err := d.db.SelectContext(ctx, &mainPlayers, mainQuery)
	if err != nil {
		return nil, err
	}

	// Get alts
	altQuery := `SELECT player_name FROM player_alts ORDER BY player_name`
	var altPlayers []string
	err = d.db.SelectContext(ctx, &altPlayers, altQuery)
	if err != nil {
		return nil, err
	}

	// Combine and deduplicate
	seen := make(map[string]bool)
	var allPlayers []string
	for _, name := range mainPlayers {
		if !seen[name] {
			seen[name] = true
			allPlayers = append(allPlayers, name)
		}
	}
	for _, name := range altPlayers {
		if !seen[name] {
			seen[name] = true
			allPlayers = append(allPlayers, name)
		}
	}

	return allPlayers, nil
}

// GetDiscordUserIDForPlayer returns the Discord user ID for a player name (checking both main and alts)
func (d *DB) GetDiscordUserIDForPlayer(ctx context.Context, playerName string) (string, error) {
	// Check main players table first
	var discordUserID string
	query := `SELECT discord_user_id FROM players WHERE player_name = ?`
	err := d.db.GetContext(ctx, &discordUserID, query, playerName)
	if err == nil {
		return discordUserID, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	
	// Check alts table
	query = `SELECT discord_user_id FROM player_alts WHERE player_name = ?`
	err = d.db.GetContext(ctx, &discordUserID, query, playerName)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("player not found")
	}
	if err != nil {
		return "", err
	}
	
	return discordUserID, nil
}

// WebSession represents a web session stored in the database
type WebSession struct {
	SessionID string    `db:"session_id"`
	UserID    string    `db:"user_id"`
	Username  string    `db:"username"`
	Avatar    string    `db:"avatar"`
	ExpiresAt time.Time `db:"expires_at"`
}

// CreateSession creates a new web session
func (d *DB) CreateSession(ctx context.Context, sessionID, userID, username, avatar string, expiresAt time.Time) error {
	query := `
		INSERT INTO web_sessions (session_id, user_id, username, avatar, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(session_id) DO UPDATE SET
			user_id = excluded.user_id,
			username = excluded.username,
			avatar = excluded.avatar,
			expires_at = excluded.expires_at
	`
	_, err := d.db.ExecContext(ctx, query, sessionID, userID, username, avatar, expiresAt)
	return err
}

// GetSession retrieves a web session by ID
func (d *DB) GetSession(ctx context.Context, sessionID string) (*WebSession, error) {
	var session WebSession
	query := `SELECT session_id, user_id, username, avatar, expires_at FROM web_sessions WHERE session_id = ?`
	err := d.db.GetContext(ctx, &session, query, sessionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// DeleteSession deletes a web session
func (d *DB) DeleteSession(ctx context.Context, sessionID string) error {
	query := `DELETE FROM web_sessions WHERE session_id = ?`
	_, err := d.db.ExecContext(ctx, query, sessionID)
	return err
}

// CleanupExpiredSessions removes expired sessions
func (d *DB) CleanupExpiredSessions(ctx context.Context) error {
	query := `DELETE FROM web_sessions WHERE expires_at < CURRENT_TIMESTAMP`
	_, err := d.db.ExecContext(ctx, query)
	return err
}
