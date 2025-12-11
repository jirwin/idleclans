package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

var (
	sqlitePath  = flag.String("sqlite", "", "Path to SQLite database file")
	postgresURL = flag.String("postgres", "", "PostgreSQL connection string (e.g., postgres://user:pass@host/dbname)")
	reset       = flag.Bool("reset", false, "Drop and recreate tables in PostgreSQL before migration")
	verifyOnly  = flag.Bool("verify-only", false, "Only verify data without migrating")
	dryRun      = flag.Bool("dry-run", false, "Show what would be migrated without actually doing it")
)

func main() {
	flag.Parse()

	if *sqlitePath == "" || *postgresURL == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s -sqlite <path> -postgres <url> [options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  -reset       Drop and recreate tables before migration\n")
		fmt.Fprintf(os.Stderr, "  -verify-only Only verify data without migrating\n")
		fmt.Fprintf(os.Stderr, "  -dry-run     Show what would be migrated\n")
		os.Exit(1)
	}

	// Connect to SQLite
	sqliteDB, err := sql.Open("sqlite3", *sqlitePath)
	if err != nil {
		log.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer sqliteDB.Close()

	// Connect to PostgreSQL
	postgresDB, err := sql.Open("postgres", *postgresURL)
	if err != nil {
		log.Fatalf("Failed to open PostgreSQL database: %v", err)
	}
	defer postgresDB.Close()

	// Test connections
	if err := sqliteDB.Ping(); err != nil {
		log.Fatalf("Failed to ping SQLite database: %v", err)
	}
	if err := postgresDB.Ping(); err != nil {
		log.Fatalf("Failed to ping PostgreSQL database: %v", err)
	}

	if *verifyOnly {
		verifyData(sqliteDB, postgresDB)
		return
	}

	if *dryRun {
		dryRunMigration(sqliteDB, postgresDB)
		return
	}

	// Check if schema exists, create if not
	if !schemaExists(postgresDB) {
		log.Println("Schema does not exist, creating...")
		if err := createPostgreSQLSchema(postgresDB); err != nil {
			log.Fatalf("Failed to create schema: %v", err)
		}
		log.Println("Schema created successfully")
	} else {
		log.Println("Schema already exists")
	}

	// Perform migration
	if err := migrateData(sqliteDB, postgresDB, *reset); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	log.Println("Migration completed successfully!")
	
	// Verify after migration
	log.Println("Verifying migrated data...")
	verifyData(sqliteDB, postgresDB)
}

func migrateData(sqliteDB, postgresDB *sql.DB, reset bool) error {
	tables := []string{
		"players",
		"player_alts",
		"player_keys",
		"weekly_quests",
		"quest_kills",
		"web_sessions",
		"parties",
		"party_step_progress",
	}

	if reset {
		log.Println("Resetting PostgreSQL tables...")
		// Drop tables in reverse dependency order
		dropOrder := []string{
			"party_step_progress",
			"parties",
			"quest_kills",
			"weekly_quests",
			"player_keys",
			"player_alts",
			"web_sessions",
			"players",
		}
		for _, table := range dropOrder {
			if _, err := postgresDB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table)); err != nil {
				log.Printf("Warning: Failed to drop table %s: %v", table, err)
			}
		}
		// Recreate schema
		if err := createPostgreSQLSchema(postgresDB); err != nil {
			return fmt.Errorf("failed to create schema: %w", err)
		}
	}

	// Migrate each table
	for _, table := range tables {
		log.Printf("Migrating table: %s", table)
		
		// For quest_kills, check what quest_ids exist in weekly_quests first
		if table == "quest_kills" {
			var existingQuestIDs []int
			rows, err := postgresDB.Query("SELECT id FROM weekly_quests ORDER BY id")
			if err == nil {
				for rows.Next() {
					var id int
					if err := rows.Scan(&id); err == nil {
						existingQuestIDs = append(existingQuestIDs, id)
					}
				}
				rows.Close()
				log.Printf("  Found %d quest IDs in weekly_quests: %v", len(existingQuestIDs), existingQuestIDs)
			}
		}
		
		if err := migrateTable(sqliteDB, postgresDB, table); err != nil {
			return fmt.Errorf("failed to migrate table %s: %w", table, err)
		}
	}

	return nil
}

func migrateTable(sqliteDB, postgresDB *sql.DB, tableName string) error {
	// Get row count from SQLite
	var sqliteCount int
	if err := sqliteDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&sqliteCount); err != nil {
		return fmt.Errorf("failed to count rows in SQLite: %w", err)
	}

	if sqliteCount == 0 {
		log.Printf("  Table %s is empty, skipping", tableName)
		return nil
	}

	// Get column names and types
	rows, err := sqliteDB.Query(fmt.Sprintf("SELECT * FROM %s LIMIT 1", tableName))
	if err != nil {
		return fmt.Errorf("failed to query SQLite: %w", err)
	}
	columns, err := rows.Columns()
	rows.Close()
	if err != nil {
		return fmt.Errorf("failed to get columns: %w", err)
	}

	// Select all data from SQLite
	query := fmt.Sprintf("SELECT * FROM %s", tableName)
	sqliteRows, err := sqliteDB.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query SQLite: %w", err)
	}
	defer sqliteRows.Close()

	// Build insert statement for PostgreSQL
	// For tables with SERIAL primary keys, we need to explicitly set the ID to preserve foreign key relationships
	placeholders := ""
	columnList := joinColumns(columns)
	
	// Check if this table has an 'id' column that's a primary key (SERIAL)
	// If so, we need to preserve IDs to maintain foreign key relationships
	hasSerialID := false
	idColumnIndex := -1
	for i, col := range columns {
		if col == "id" {
			hasSerialID = true
			idColumnIndex = i
			break
		}
	}
	
	for i := range columns {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += fmt.Sprintf("$%d", i+1)
	}
	insertQuery := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableName, columnList, placeholders)

	// Begin transaction
	tx, err := postgresDB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// For tables with SERIAL primary keys that are referenced by foreign keys,
	// we need to preserve the ID values by temporarily disabling the sequence
	if hasSerialID && (tableName == "weekly_quests" || tableName == "quest_kills") {
		// Get the sequence name for this table's id column
		seqName := fmt.Sprintf("%s_id_seq", tableName)
		// Temporarily set the sequence to allow manual ID insertion
		// Set it to 1 (false means don't use the value yet)
		if _, err := tx.Exec(fmt.Sprintf("SELECT setval('%s', 1, false)", seqName)); err != nil {
			log.Printf("Warning: Failed to reset sequence %s (may not exist yet): %v", seqName, err)
		}
	}

	stmt, err := tx.Prepare(insertQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// Copy data
	rowCount := 0
	skippedCount := 0
	maxID := 0
	rowNum := 0
	for sqliteRows.Next() {
		rowNum++
		// Create a savepoint for each row so we can rollback just this row on error
		savepointName := fmt.Sprintf("sp_%s_%d", tableName, rowNum)
		if _, err := tx.Exec(fmt.Sprintf("SAVEPOINT %s", savepointName)); err != nil {
			log.Printf("  Warning: Failed to create savepoint for row %d: %v", rowNum, err)
			skippedCount++
			continue
		}

		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := sqliteRows.Scan(valuePtrs...); err != nil {
			tx.Exec(fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", savepointName))
			log.Printf("  Warning: Failed to scan row %d: %v", rowNum, err)
			skippedCount++
			continue
		}

		// Track max ID for sequence update
		if hasSerialID && idColumnIndex >= 0 {
			if idVal, ok := values[idColumnIndex].(int64); ok {
				if int(idVal) > maxID {
					maxID = int(idVal)
				}
			} else if idVal, ok := values[idColumnIndex].(int); ok {
				if idVal > maxID {
					maxID = idVal
				}
			} else if idVal, ok := values[idColumnIndex].(*int64); ok && idVal != nil {
				if int(*idVal) > maxID {
					maxID = int(*idVal)
				}
			} else if idVal, ok := values[idColumnIndex].(*int); ok && idVal != nil {
				if *idVal > maxID {
					maxID = *idVal
				}
			}
		}

		// Convert values for PostgreSQL (handle NULL, time, etc.)
		pgValues := make([]interface{}, len(values))
		for i, v := range values {
			pgValues[i] = convertValue(v)
		}

		// Log row details for debugging (especially for foreign key issues)
		rowDetails := make(map[string]interface{})
		for i, col := range columns {
			rowDetails[col] = pgValues[i]
		}
		
		if _, err := stmt.Exec(pgValues...); err != nil {
			// Rollback to savepoint to continue with next row
			tx.Exec(fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", savepointName))
			log.Printf("  Warning: Failed to insert row %d (skipping): %v", rowNum, err)
			log.Printf("    Row data: %+v", rowDetails)
			skippedCount++
			continue
		}
		
		// Release savepoint on success
		if _, err := tx.Exec(fmt.Sprintf("RELEASE SAVEPOINT %s", savepointName)); err != nil {
			log.Printf("  Warning: Failed to release savepoint for row %d: %v", rowNum, err)
		}
		
		rowCount++
	}

	if err := sqliteRows.Err(); err != nil {
		return fmt.Errorf("error reading rows: %w", err)
	}

	// Update sequence to be after the max ID we inserted
	if hasSerialID && maxID > 0 {
		seqName := fmt.Sprintf("%s_id_seq", tableName)
		if _, err := tx.Exec(fmt.Sprintf("SELECT setval('%s', %d, true)", seqName, maxID)); err != nil {
			log.Printf("  Warning: Failed to update sequence %s: %v", seqName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	if skippedCount > 0 {
		log.Printf("  Migrated %d rows from %s (skipped %d rows due to errors)", rowCount, tableName, skippedCount)
	} else {
		log.Printf("  Migrated %d rows from %s", rowCount, tableName)
	}
	return nil
}

func convertValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	// Handle pointers - dereference them
	switch val := v.(type) {
	case *int64:
		if val == nil {
			return nil
		}
		return *val
	case *int:
		if val == nil {
			return nil
		}
		return *val
	case *string:
		if val == nil {
			return nil
		}
		return *val
	case *time.Time:
		if val == nil {
			return nil
		}
		return *val
	case *bool:
		if val == nil {
			return nil
		}
		return *val
	}
	// Handle time.Time conversion
	if t, ok := v.(time.Time); ok {
		return t
	}
	// Handle []byte (for TEXT/BLOB)
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

func joinColumns(columns []string) string {
	result := ""
	for i, col := range columns {
		if i > 0 {
			result += ", "
		}
		result += col
	}
	return result
}

func schemaExists(db *sql.DB) bool {
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'players'
		)
	`).Scan(&exists)
	return err == nil && exists
}

func createPostgreSQLSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS players (
		discord_user_id TEXT PRIMARY KEY,
		player_name TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS weekly_quests (
		id SERIAL PRIMARY KEY,
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
		UNIQUE(discord_user_id, player_name, week_number, year, boss_name)
	);

	CREATE TABLE IF NOT EXISTS quest_kills (
		id SERIAL PRIMARY KEY,
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

	CREATE TABLE IF NOT EXISTS parties (
		id TEXT PRIMARY KEY,
		players TEXT NOT NULL,
		plan_data TEXT NOT NULL,
		current_step_index INTEGER DEFAULT 0,
		started_at TIMESTAMP,
		ended_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS party_step_progress (
		party_id TEXT NOT NULL,
		step_index INTEGER NOT NULL,
		boss_name TEXT NOT NULL,
		kills_tracked INTEGER DEFAULT 0,
		keys_used INTEGER DEFAULT 0,
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		PRIMARY KEY (party_id, step_index),
		FOREIGN KEY (party_id) REFERENCES parties(id)
	);

	CREATE INDEX IF NOT EXISTS idx_parties_created ON parties(created_at);
	CREATE INDEX IF NOT EXISTS idx_party_step_progress_party ON party_step_progress(party_id);
	`

	_, err := db.Exec(schema)
	return err
}

func verifyData(sqliteDB, postgresDB *sql.DB) {
	tables := []string{
		"players",
		"player_alts",
		"player_keys",
		"weekly_quests",
		"quest_kills",
		"web_sessions",
		"parties",
		"party_step_progress",
	}

	allMatch := true
	for _, table := range tables {
		var sqliteCount, postgresCount int
		
		if err := sqliteDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&sqliteCount); err != nil {
			log.Printf("Warning: Failed to count SQLite %s: %v", table, err)
			continue
		}
		
		if err := postgresDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&postgresCount); err != nil {
			log.Printf("Warning: Failed to count PostgreSQL %s: %v", table, err)
			continue
		}

		match := sqliteCount == postgresCount
		status := "✓"
		if !match {
			status = "✗"
			allMatch = false
		}
		
		log.Printf("%s %s: SQLite=%d, PostgreSQL=%d", status, table, sqliteCount, postgresCount)
	}

	if allMatch {
		log.Println("All table row counts match!")
	} else {
		log.Println("WARNING: Some table row counts do not match!")
	}
}

func dryRunMigration(sqliteDB, postgresDB *sql.DB) {
	tables := []string{
		"players",
		"player_alts",
		"player_keys",
		"weekly_quests",
		"quest_kills",
		"web_sessions",
		"parties",
		"party_step_progress",
	}

	log.Println("Dry run - would migrate the following:")
	for _, table := range tables {
		var count int
		if err := sqliteDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count); err != nil {
			log.Printf("  %s: ERROR - %v", table, err)
			continue
		}
		log.Printf("  %s: %d rows", table, count)
	}
}

