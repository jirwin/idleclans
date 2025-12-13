package market

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

// DB handles market data storage
type DB struct {
	db     *sqlx.DB
	logger *zap.Logger
}

// NewDB creates a new market database connection using an existing sqlx.DB
func NewDB(db *sqlx.DB, logger *zap.Logger) (*DB, error) {
	d := &DB{db: db, logger: logger}
	if err := d.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize market schema: %w", err)
	}
	return d, nil
}

// NewDBFromConnection creates a new market database from a connection string
func NewDBFromConnection(connectionString string, logger *zap.Logger) (*DB, error) {
	if !strings.HasPrefix(connectionString, "postgres://") && !strings.HasPrefix(connectionString, "postgresql://") {
		return nil, fmt.Errorf("only PostgreSQL connection strings are supported")
	}

	db, err := sqlx.Open("postgres", connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return NewDB(db, logger)
}

func (d *DB) initSchema() error {
	// First run the base schema (creates tables without new columns)
	baseSchema := d.getBaseSchema()
	_, err := d.db.Exec(baseSchema)
	if err != nil {
		return fmt.Errorf("failed to create base schema: %w", err)
	}

	// Run migrations to add new columns to existing tables
	d.runMigrations()

	// Try to enable TimescaleDB if available
	d.tryEnableTimescale()

	return nil
}

// runMigrations applies schema changes for existing databases
func (d *DB) runMigrations() {
	// Add price_last_collected column if it doesn't exist
	_, err := d.db.Exec(`
		ALTER TABLE market_items 
		ADD COLUMN IF NOT EXISTS price_last_collected TIMESTAMPTZ
	`)
	if err != nil {
		d.logger.Debug("Migration: price_last_collected column may already exist", zap.Error(err))
	}

	// Add history_backfilled column if it doesn't exist
	_, err = d.db.Exec(`
		ALTER TABLE market_items 
		ADD COLUMN IF NOT EXISTS history_backfilled BOOLEAN DEFAULT FALSE
	`)
	if err != nil {
		d.logger.Debug("Migration: history_backfilled column may already exist", zap.Error(err))
	}

	// Add index for priority queue queries
	_, _ = d.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_market_items_price_last_collected 
		ON market_items(price_last_collected NULLS FIRST)
	`)

	// Add optimized indexes for market overview queries
	// Covering index for recent prices lookups - critical for top movers queries
	_, _ = d.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_market_prices_time_item_price 
		ON market_prices(time DESC, item_id, lowest_sell_price, lowest_price_volume)
		WHERE lowest_sell_price > 0
	`)

	// Index for volume queries
	_, _ = d.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_market_prices_volume
		ON market_prices(item_id, time DESC, lowest_price_volume DESC)
		WHERE lowest_price_volume > 0
	`)
}

func (d *DB) getBaseSchema() string {
	return `
	-- Item metadata cache
	CREATE TABLE IF NOT EXISTS market_items (
		id INTEGER PRIMARY KEY,
		name_id TEXT NOT NULL UNIQUE,
		display_name TEXT,
		category TEXT,
		updated_at TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_market_items_name_id ON market_items(name_id);
	CREATE INDEX IF NOT EXISTS idx_market_items_category ON market_items(category);

	-- Price snapshots (will be converted to hypertable if TimescaleDB is available)
	CREATE TABLE IF NOT EXISTS market_prices (
		time TIMESTAMPTZ NOT NULL,
		item_id INTEGER NOT NULL REFERENCES market_items(id),
		lowest_sell_price INTEGER,
		lowest_price_volume INTEGER,
		highest_buy_price INTEGER,
		highest_price_volume INTEGER,
		PRIMARY KEY (time, item_id)
	);

	CREATE INDEX IF NOT EXISTS idx_market_prices_item_time ON market_prices(item_id, time DESC);

	-- Daily price aggregates for faster long-term queries
	CREATE TABLE IF NOT EXISTS market_price_daily (
		date DATE NOT NULL,
		item_id INTEGER NOT NULL REFERENCES market_items(id),
		open_price INTEGER,
		high_price INTEGER,
		low_price INTEGER,
		close_price INTEGER,
		avg_price NUMERIC(12,2),
		total_sell_volume BIGINT,
		total_buy_volume BIGINT,
		sample_count INTEGER,
		PRIMARY KEY (date, item_id)
	);

	CREATE INDEX IF NOT EXISTS idx_market_price_daily_item ON market_price_daily(item_id, date DESC);

	-- Trade history from the API (if available)
	CREATE TABLE IF NOT EXISTS market_trade_history (
		time TIMESTAMPTZ NOT NULL,
		item_id INTEGER NOT NULL REFERENCES market_items(id),
		lowest_sell_price INTEGER,
		highest_sell_price INTEGER,
		average_price INTEGER,
		trade_volume NUMERIC(12,2),
		PRIMARY KEY (time, item_id)
	);

	CREATE INDEX IF NOT EXISTS idx_market_trade_history_item ON market_trade_history(item_id, time DESC);

	-- Collector state tracking
	CREATE TABLE IF NOT EXISTS market_collector_state (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TIMESTAMPTZ DEFAULT NOW()
	);

	-- Cached market overview for fast loading (pre-computed by collector)
	CREATE TABLE IF NOT EXISTS market_overview_cache (
		id INTEGER PRIMARY KEY DEFAULT 1,
		total_items INTEGER NOT NULL DEFAULT 0,
		active_items INTEGER NOT NULL DEFAULT 0,
		top_gainers JSONB NOT NULL DEFAULT '[]',
		top_losers JSONB NOT NULL DEFAULT '[]',
		most_traded JSONB NOT NULL DEFAULT '[]',
		updated_at TIMESTAMPTZ DEFAULT NOW(),
		CONSTRAINT single_row CHECK (id = 1)
	);

	-- Initialize the cache row if it doesn't exist
	INSERT INTO market_overview_cache (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
	`
}

// tryEnableTimescale attempts to enable TimescaleDB extension and convert tables
func (d *DB) tryEnableTimescale() {
	// Try to create TimescaleDB extension
	_, err := d.db.Exec(`CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE`)
	if err != nil {
		d.logger.Debug("TimescaleDB not available, using regular PostgreSQL tables", zap.Error(err))
		return
	}

	// Check if market_prices is already a hypertable
	var isHypertable bool
	err = d.db.Get(&isHypertable, `
		SELECT EXISTS (
			SELECT 1 FROM timescaledb_information.hypertables 
			WHERE hypertable_name = 'market_prices'
		)
	`)
	if err != nil {
		d.logger.Debug("Could not check hypertable status", zap.Error(err))
		return
	}

	if !isHypertable {
		// Convert to hypertable
		_, err = d.db.Exec(`SELECT create_hypertable('market_prices', 'time', if_not_exists => TRUE, migrate_data => TRUE)`)
		if err != nil {
			d.logger.Warn("Failed to create hypertable for market_prices", zap.Error(err))
		} else {
			d.logger.Info("Created TimescaleDB hypertable for market_prices")
		}
	}

	// Check market_trade_history
	err = d.db.Get(&isHypertable, `
		SELECT EXISTS (
			SELECT 1 FROM timescaledb_information.hypertables 
			WHERE hypertable_name = 'market_trade_history'
		)
	`)
	if err == nil && !isHypertable {
		_, err = d.db.Exec(`SELECT create_hypertable('market_trade_history', 'time', if_not_exists => TRUE, migrate_data => TRUE)`)
		if err != nil {
			d.logger.Warn("Failed to create hypertable for market_trade_history", zap.Error(err))
		} else {
			d.logger.Info("Created TimescaleDB hypertable for market_trade_history")
		}
	}

	// Set up compression policy (compress data older than 7 days)
	_, _ = d.db.Exec(`
		ALTER TABLE market_prices SET (
			timescaledb.compress,
			timescaledb.compress_segmentby = 'item_id'
		)
	`)
	_, _ = d.db.Exec(`SELECT add_compression_policy('market_prices', INTERVAL '7 days', if_not_exists => TRUE)`)

	// Set up retention policy (keep 1 year of raw data)
	_, _ = d.db.Exec(`SELECT add_retention_policy('market_prices', INTERVAL '1 year', if_not_exists => TRUE)`)

	// Create continuous aggregate for hourly rollup
	// This automatically aggregates per-minute data into hourly buckets
	d.createContinuousAggregates()
}

// createContinuousAggregates sets up TimescaleDB continuous aggregates for efficient time-based queries
func (d *DB) createContinuousAggregates() {
	// Check if hourly aggregate already exists
	var exists bool
	err := d.db.Get(&exists, `
		SELECT EXISTS (
			SELECT 1 FROM timescaledb_information.continuous_aggregates 
			WHERE view_name = 'market_prices_hourly'
		)
	`)
	if err != nil {
		d.logger.Debug("Could not check for continuous aggregate", zap.Error(err))
		return
	}

	if !exists {
		// Create hourly continuous aggregate
		_, err = d.db.Exec(`
			CREATE MATERIALIZED VIEW market_prices_hourly
			WITH (timescaledb.continuous) AS
			SELECT 
				time_bucket('1 hour', time) AS bucket,
				item_id,
				first(lowest_sell_price, time) AS open_price,
				max(lowest_sell_price) AS high_price,
				min(NULLIF(lowest_sell_price, 0)) AS low_price,
				last(lowest_sell_price, time) AS close_price,
				avg(lowest_sell_price)::integer AS avg_price,
				sum(lowest_price_volume) AS total_volume,
				count(*) AS sample_count
			FROM market_prices
			GROUP BY bucket, item_id
			WITH NO DATA
		`)
		if err != nil {
			d.logger.Warn("Failed to create hourly continuous aggregate", zap.Error(err))
		} else {
			d.logger.Info("Created hourly continuous aggregate")

			// Add refresh policy - refresh every 30 minutes, covering last 2 days
			_, _ = d.db.Exec(`
				SELECT add_continuous_aggregate_policy('market_prices_hourly',
					start_offset => INTERVAL '2 days',
					end_offset => INTERVAL '1 hour',
					schedule_interval => INTERVAL '30 minutes',
					if_not_exists => TRUE
				)
			`)
		}
	}

	// Check if daily aggregate already exists
	err = d.db.Get(&exists, `
		SELECT EXISTS (
			SELECT 1 FROM timescaledb_information.continuous_aggregates 
			WHERE view_name = 'market_prices_daily'
		)
	`)
	if err != nil {
		return
	}

	if !exists {
		// Create daily continuous aggregate
		_, err = d.db.Exec(`
			CREATE MATERIALIZED VIEW market_prices_daily
			WITH (timescaledb.continuous) AS
			SELECT 
				time_bucket('1 day', time) AS bucket,
				item_id,
				first(lowest_sell_price, time) AS open_price,
				max(lowest_sell_price) AS high_price,
				min(NULLIF(lowest_sell_price, 0)) AS low_price,
				last(lowest_sell_price, time) AS close_price,
				avg(lowest_sell_price)::integer AS avg_price,
				sum(lowest_price_volume) AS total_volume,
				count(*) AS sample_count
			FROM market_prices
			GROUP BY bucket, item_id
			WITH NO DATA
		`)
		if err != nil {
			d.logger.Warn("Failed to create daily continuous aggregate", zap.Error(err))
		} else {
			d.logger.Info("Created daily continuous aggregate")

			// Add refresh policy - refresh every 2 hours, covering last 7 days
			_, _ = d.db.Exec(`
				SELECT add_continuous_aggregate_policy('market_prices_daily',
					start_offset => INTERVAL '7 days',
					end_offset => INTERVAL '1 day',
					schedule_interval => INTERVAL '2 hours',
					if_not_exists => TRUE
				)
			`)
		}
	}
}

// Item represents an item in the market
type Item struct {
	ID                 int        `db:"id" json:"id"`
	NameID             string     `db:"name_id" json:"name_id"`
	DisplayName        string     `db:"display_name" json:"display_name"`
	Category           string     `db:"category" json:"category"`
	UpdatedAt          time.Time  `db:"updated_at" json:"updated_at"`
	PriceLastCollected *time.Time `db:"price_last_collected" json:"price_last_collected,omitempty"`
	HistoryBackfilled  bool       `db:"history_backfilled" json:"history_backfilled"`
}

// PriceSnapshot represents a point-in-time price snapshot
type PriceSnapshot struct {
	Time               time.Time `db:"time" json:"time"`
	ItemID             int       `db:"item_id" json:"item_id"`
	LowestSellPrice    int       `db:"lowest_sell_price" json:"lowest_sell_price"`
	LowestPriceVolume  int       `db:"lowest_price_volume" json:"lowest_price_volume"`
	HighestBuyPrice    int       `db:"highest_buy_price" json:"highest_buy_price"`
	HighestPriceVolume int       `db:"highest_price_volume" json:"highest_price_volume"`
}

// DailyAggregate represents daily price aggregates
type DailyAggregate struct {
	Date            time.Time `db:"date" json:"date"`
	ItemID          int       `db:"item_id" json:"item_id"`
	OpenPrice       int       `db:"open_price" json:"open_price"`
	HighPrice       int       `db:"high_price" json:"high_price"`
	LowPrice        int       `db:"low_price" json:"low_price"`
	ClosePrice      int       `db:"close_price" json:"close_price"`
	AvgPrice        float64   `db:"avg_price" json:"avg_price"`
	TotalSellVolume int64     `db:"total_sell_volume" json:"total_sell_volume"`
	TotalBuyVolume  int64     `db:"total_buy_volume" json:"total_buy_volume"`
	SampleCount     int       `db:"sample_count" json:"sample_count"`
}

// TradeHistory represents trade history from the API
type TradeHistory struct {
	Time             time.Time `db:"time" json:"time"`
	ItemID           int       `db:"item_id" json:"item_id"`
	LowestSellPrice  int       `db:"lowest_sell_price" json:"lowest_sell_price"`
	HighestSellPrice int       `db:"highest_sell_price" json:"highest_sell_price"`
	AveragePrice     int       `db:"average_price" json:"average_price"`
	TradeVolume      float64   `db:"trade_volume" json:"trade_volume"`
}

// UpsertItem creates or updates an item
func (d *DB) UpsertItem(ctx context.Context, id int, nameID, displayName, category string) error {
	query := `
		INSERT INTO market_items (id, name_id, display_name, category, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (id) DO UPDATE SET
			name_id = EXCLUDED.name_id,
			display_name = EXCLUDED.display_name,
			category = COALESCE(EXCLUDED.category, market_items.category),
			updated_at = NOW()
	`
	_, err := d.db.ExecContext(ctx, query, id, nameID, displayName, category)
	return err
}

// GetItem retrieves an item by ID
func (d *DB) GetItem(ctx context.Context, id int) (*Item, error) {
	var item Item
	err := d.db.GetContext(ctx, &item, `SELECT * FROM market_items WHERE id = $1`, id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &item, err
}

// GetItemByNameID retrieves an item by name_id
func (d *DB) GetItemByNameID(ctx context.Context, nameID string) (*Item, error) {
	var item Item
	err := d.db.GetContext(ctx, &item, `SELECT * FROM market_items WHERE name_id = $1`, nameID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &item, err
}

// GetItemsNeedingPriceUpdate returns items sorted by oldest price collection (NULLS FIRST)
// This implements a priority queue - items that have never been collected come first,
// then items with the oldest data
func (d *DB) GetItemsNeedingPriceUpdate(ctx context.Context, limit int) ([]Item, error) {
	var items []Item
	query := `
		SELECT * FROM market_items 
		ORDER BY price_last_collected NULLS FIRST, updated_at ASC
		LIMIT $1
	`
	err := d.db.SelectContext(ctx, &items, query, limit)
	return items, err
}

// GetItemsNeedingHistoryBackfill returns items that haven't had their history backfilled yet
func (d *DB) GetItemsNeedingHistoryBackfill(ctx context.Context, limit int) ([]Item, error) {
	var items []Item
	query := `
		SELECT * FROM market_items 
		WHERE history_backfilled = FALSE
		ORDER BY updated_at ASC
		LIMIT $1
	`
	err := d.db.SelectContext(ctx, &items, query, limit)
	return items, err
}

// UpdateItemPriceCollected marks an item's price as collected
func (d *DB) UpdateItemPriceCollected(ctx context.Context, itemID int) error {
	query := `UPDATE market_items SET price_last_collected = NOW() WHERE id = $1`
	_, err := d.db.ExecContext(ctx, query, itemID)
	return err
}

// MarkItemHistoryBackfilled marks an item as having its history backfilled
func (d *DB) MarkItemHistoryBackfilled(ctx context.Context, itemID int) error {
	query := `UPDATE market_items SET history_backfilled = TRUE WHERE id = $1`
	_, err := d.db.ExecContext(ctx, query, itemID)
	return err
}

// ResetBackfillFlags resets the history_backfilled flag for all items
// This allows a forced re-backfill of all items
func (d *DB) ResetBackfillFlags(ctx context.Context) error {
	query := `UPDATE market_items SET history_backfilled = FALSE`
	_, err := d.db.ExecContext(ctx, query)
	return err
}

// GetAllItems retrieves all items
func (d *DB) GetAllItems(ctx context.Context) ([]Item, error) {
	var items []Item
	// Only return items that have price data
	query := `
		SELECT DISTINCT mi.* FROM market_items mi
		INNER JOIN market_prices mp ON mi.id = mp.item_id
		ORDER BY mi.name_id
	`
	err := d.db.SelectContext(ctx, &items, query)
	return items, err
}

// ItemWithPrice represents an item with its latest price data
type ItemWithPrice struct {
	ID                 int     `json:"id" db:"id"`
	NameID             string  `json:"name_id" db:"name_id"`
	DisplayName        string  `json:"display_name" db:"display_name"`
	Category           string  `json:"category" db:"category"`
	LowestSellPrice    int     `json:"lowest_sell_price" db:"lowest_sell_price"`
	LowestPriceVolume  int     `json:"lowest_price_volume" db:"lowest_price_volume"`
	HighestBuyPrice    int     `json:"highest_buy_price" db:"highest_buy_price"`
	HighestPriceVolume int     `json:"highest_price_volume" db:"highest_price_volume"`
	Spread             int     `json:"spread" db:"spread"`
	SpreadPercent      float64 `json:"spread_percent" db:"spread_percent"`
	LastUpdated        string  `json:"last_updated" db:"last_updated"`
}

// GetAllItemsWithPrices retrieves all items with their latest prices
func (d *DB) GetAllItemsWithPrices(ctx context.Context) ([]ItemWithPrice, error) {
	var items []ItemWithPrice
	query := `
		WITH latest_prices AS (
			SELECT DISTINCT ON (item_id)
				item_id,
				lowest_sell_price,
				lowest_price_volume,
				highest_buy_price,
				highest_price_volume,
				time
			FROM market_prices
			ORDER BY item_id, time DESC
		)
		SELECT 
			mi.id,
			mi.name_id,
			mi.display_name,
			mi.category,
			COALESCE(lp.lowest_sell_price, 0) as lowest_sell_price,
			COALESCE(lp.lowest_price_volume, 0) as lowest_price_volume,
			COALESCE(lp.highest_buy_price, 0) as highest_buy_price,
			COALESCE(lp.highest_price_volume, 0) as highest_price_volume,
			CASE 
				WHEN lp.highest_buy_price > 0 THEN lp.lowest_sell_price - lp.highest_buy_price
				ELSE 0
			END as spread,
			CASE 
				WHEN lp.highest_buy_price > 0 THEN 
					((lp.lowest_sell_price - lp.highest_buy_price)::float / lp.highest_buy_price) * 100
				ELSE 0
			END as spread_percent,
			COALESCE(to_char(lp.time, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '') as last_updated
		FROM market_items mi
		INNER JOIN latest_prices lp ON mi.id = lp.item_id
		ORDER BY mi.display_name
	`
	err := d.db.SelectContext(ctx, &items, query)
	return items, err
}

// PaginatedItemsResult holds paginated items with total count
type PaginatedItemsResult struct {
	Items []ItemWithPrice `json:"items"`
	Total int             `json:"total"`
}

// GetAllItemsWithPricesPaginated retrieves items with their latest prices with pagination
func (d *DB) GetAllItemsWithPricesPaginated(ctx context.Context, offset, limit int) (*PaginatedItemsResult, error) {
	// First get the total count
	var total int
	countQuery := `
		SELECT COUNT(DISTINCT mi.id)
		FROM market_items mi
		INNER JOIN market_prices mp ON mi.id = mp.item_id
	`
	if err := d.db.GetContext(ctx, &total, countQuery); err != nil {
		return nil, err
	}

	var items []ItemWithPrice
	query := `
		WITH latest_prices AS (
			SELECT DISTINCT ON (item_id)
				item_id,
				lowest_sell_price,
				lowest_price_volume,
				highest_buy_price,
				highest_price_volume,
				time
			FROM market_prices
			ORDER BY item_id, time DESC
		)
		SELECT 
			mi.id,
			mi.name_id,
			mi.display_name,
			mi.category,
			COALESCE(lp.lowest_sell_price, 0) as lowest_sell_price,
			COALESCE(lp.lowest_price_volume, 0) as lowest_price_volume,
			COALESCE(lp.highest_buy_price, 0) as highest_buy_price,
			COALESCE(lp.highest_price_volume, 0) as highest_price_volume,
			CASE 
				WHEN lp.highest_buy_price > 0 THEN lp.lowest_sell_price - lp.highest_buy_price
				ELSE 0
			END as spread,
			CASE 
				WHEN lp.highest_buy_price > 0 THEN 
					((lp.lowest_sell_price - lp.highest_buy_price)::float / lp.highest_buy_price) * 100
				ELSE 0
			END as spread_percent,
			COALESCE(to_char(lp.time, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '') as last_updated
		FROM market_items mi
		INNER JOIN latest_prices lp ON mi.id = lp.item_id
		ORDER BY mi.display_name
		LIMIT $1 OFFSET $2
	`
	if err := d.db.SelectContext(ctx, &items, query, limit, offset); err != nil {
		return nil, err
	}

	return &PaginatedItemsResult{
		Items: items,
		Total: total,
	}, nil
}

// SearchItems searches items by name (only items with price data)
func (d *DB) SearchItems(ctx context.Context, query string, limit int) ([]Item, error) {
	var items []Item
	searchQuery := `
		SELECT DISTINCT mi.* FROM market_items mi
		INNER JOIN market_prices mp ON mi.id = mp.item_id
		WHERE mi.name_id ILIKE $1 OR mi.display_name ILIKE $1
		ORDER BY mi.name_id
		LIMIT $2
	`
	err := d.db.SelectContext(ctx, &items, searchQuery, "%"+query+"%", limit)
	return items, err
}

// InsertPriceSnapshot inserts a price snapshot
func (d *DB) InsertPriceSnapshot(ctx context.Context, snapshot *PriceSnapshot) error {
	query := `
		INSERT INTO market_prices (time, item_id, lowest_sell_price, lowest_price_volume, highest_buy_price, highest_price_volume)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (time, item_id) DO UPDATE SET
			lowest_sell_price = EXCLUDED.lowest_sell_price,
			lowest_price_volume = EXCLUDED.lowest_price_volume,
			highest_buy_price = EXCLUDED.highest_buy_price,
			highest_price_volume = EXCLUDED.highest_price_volume
	`
	_, err := d.db.ExecContext(ctx, query,
		snapshot.Time, snapshot.ItemID,
		snapshot.LowestSellPrice, snapshot.LowestPriceVolume,
		snapshot.HighestBuyPrice, snapshot.HighestPriceVolume)
	return err
}

// InsertPriceSnapshotBatch inserts multiple price snapshots efficiently
func (d *DB) InsertPriceSnapshotBatch(ctx context.Context, snapshots []PriceSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}

	query := `
		INSERT INTO market_prices (time, item_id, lowest_sell_price, lowest_price_volume, highest_buy_price, highest_price_volume)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (time, item_id) DO UPDATE SET
			lowest_sell_price = EXCLUDED.lowest_sell_price,
			lowest_price_volume = EXCLUDED.lowest_price_volume,
			highest_buy_price = EXCLUDED.highest_buy_price,
			highest_price_volume = EXCLUDED.highest_price_volume
	`

	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PreparexContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range snapshots {
		_, err = stmt.ExecContext(ctx, s.Time, s.ItemID, s.LowestSellPrice, s.LowestPriceVolume, s.HighestBuyPrice, s.HighestPriceVolume)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetPriceHistory retrieves price history for an item
// Uses combined data from market_prices and market_trade_history tables
// to ensure backfilled historical data is included
func (d *DB) GetPriceHistory(ctx context.Context, itemID int, from, to time.Time, limit int) ([]PriceSnapshot, error) {
	duration := to.Sub(from)

	// For longer time ranges, use bucketed queries to reduce data points
	// This is more efficient and produces cleaner charts
	if duration > 7*24*time.Hour {
		// 7+ days: use 4-hour buckets
		return d.getPriceHistoryBucketedCombined(ctx, itemID, from, to, 240, limit)
	}

	if duration > 24*time.Hour {
		// 1-7 days: use 1-hour buckets
		return d.getPriceHistoryBucketedCombined(ctx, itemID, from, to, 60, limit)
	}

	if duration > 2*time.Hour {
		// 2-24 hours: use 5-minute buckets (288 points max for 24h)
		return d.getPriceHistoryBucketedCombined(ctx, itemID, from, to, 5, limit)
	}

	// For 2 hours or less, use raw per-minute data
	return d.getPriceHistoryRaw(ctx, itemID, from, to, limit)
}

// getPriceHistoryBucketedCombined retrieves price history with time bucketing from combined data sources
// This combines both market_prices (real-time) and market_trade_history (backfilled) tables
func (d *DB) getPriceHistoryBucketedCombined(ctx context.Context, itemID int, from, to time.Time, intervalMinutes int, limit int) ([]PriceSnapshot, error) {
	var snapshots []PriceSnapshot

	query := fmt.Sprintf(`
		WITH combined AS (
			-- Recent collector snapshots from market_prices
			SELECT time, item_id, lowest_sell_price, lowest_price_volume, highest_buy_price, highest_price_volume
			FROM market_prices
			WHERE item_id = $1 AND time >= $2 AND time <= $3 AND lowest_sell_price > 0
			
			UNION ALL
			
			-- Historical backfilled data from market_trade_history
			SELECT time, item_id, 
				COALESCE(NULLIF(lowest_sell_price, 0), average_price) as lowest_sell_price, 
				COALESCE(trade_volume, 0)::integer as lowest_price_volume,
				COALESCE(NULLIF(highest_sell_price, 0), average_price) as highest_buy_price,
				COALESCE(trade_volume, 0)::integer as highest_price_volume
			FROM market_trade_history
			WHERE item_id = $1 AND time >= $2 AND time <= $3 AND (lowest_sell_price > 0 OR average_price > 0)
		),
		bucketed AS (
			SELECT 
				time_bucket('%d minutes', time) as bucket_time,
				item_id,
				first(lowest_sell_price, time) as open_sell_price,
				last(lowest_sell_price, time) as close_sell_price,
				max(lowest_sell_price) as high_sell_price,
				min(NULLIF(lowest_sell_price, 0)) as low_sell_price,
				first(highest_buy_price, time) as open_buy_price,
				last(highest_buy_price, time) as close_buy_price,
				sum(lowest_price_volume) as total_sell_volume,
				sum(highest_price_volume) as total_buy_volume
			FROM combined
			WHERE lowest_sell_price > 0
			GROUP BY bucket_time, item_id
		)
		SELECT 
			bucket_time as time,
			item_id,
			COALESCE(close_sell_price, 0) as lowest_sell_price,
			COALESCE(total_sell_volume, 0)::integer as lowest_price_volume,
			COALESCE(close_buy_price, 0) as highest_buy_price,
			COALESCE(total_buy_volume, 0)::integer as highest_price_volume
		FROM bucketed
		ORDER BY bucket_time ASC
		LIMIT $4
	`, intervalMinutes)

	err := d.db.SelectContext(ctx, &snapshots, query, itemID, from, to, limit)
	return snapshots, err
}

// getPriceHistoryRaw retrieves raw per-minute price history
func (d *DB) getPriceHistoryRaw(ctx context.Context, itemID int, from, to time.Time, limit int) ([]PriceSnapshot, error) {
	var snapshots []PriceSnapshot
	// Union both tables to get full history
	// market_prices has current snapshots, market_trade_history has backfilled data
	query := `
		WITH combined AS (
			-- Recent collector snapshots
			SELECT time, item_id, lowest_sell_price, lowest_price_volume, highest_buy_price, highest_price_volume
			FROM market_prices
			WHERE item_id = $1 AND time >= $2 AND time <= $3
			
			UNION ALL
			
			-- Historical backfilled data from API
			SELECT time, item_id, 
				COALESCE(lowest_sell_price, average_price) as lowest_sell_price, 
				COALESCE(trade_volume, 0)::integer as lowest_price_volume,
				COALESCE(highest_sell_price, average_price) as highest_buy_price,
				COALESCE(trade_volume, 0)::integer as highest_price_volume
			FROM market_trade_history
			WHERE item_id = $1 AND time >= $2 AND time <= $3
		)
		SELECT DISTINCT ON (time) time, item_id, lowest_sell_price, lowest_price_volume, highest_buy_price, highest_price_volume
		FROM combined
		ORDER BY time ASC
		LIMIT $4
	`
	err := d.db.SelectContext(ctx, &snapshots, query, itemID, from, to, limit)
	return snapshots, err
}

// getPriceHistoryFromHourlyAggregate retrieves price history from hourly continuous aggregate
func (d *DB) getPriceHistoryFromHourlyAggregate(ctx context.Context, itemID int, from, to time.Time, limit int) ([]PriceSnapshot, error) {
	var snapshots []PriceSnapshot
	query := `
		SELECT 
			bucket as time,
			item_id,
			close_price as lowest_sell_price,
			total_volume::integer as lowest_price_volume,
			close_price as highest_buy_price,
			0 as highest_price_volume
		FROM market_prices_hourly
		WHERE item_id = $1 AND bucket >= $2 AND bucket <= $3
		ORDER BY bucket ASC
		LIMIT $4
	`
	err := d.db.SelectContext(ctx, &snapshots, query, itemID, from, to, limit)
	return snapshots, err
}

// getPriceHistoryFromDailyAggregate retrieves price history from daily continuous aggregate
func (d *DB) getPriceHistoryFromDailyAggregate(ctx context.Context, itemID int, from, to time.Time, limit int) ([]PriceSnapshot, error) {
	var snapshots []PriceSnapshot
	query := `
		SELECT 
			bucket as time,
			item_id,
			close_price as lowest_sell_price,
			total_volume::integer as lowest_price_volume,
			close_price as highest_buy_price,
			0 as highest_price_volume
		FROM market_prices_daily
		WHERE item_id = $1 AND bucket >= $2 AND bucket <= $3
		ORDER BY bucket ASC
		LIMIT $4
	`
	err := d.db.SelectContext(ctx, &snapshots, query, itemID, from, to, limit)
	return snapshots, err
}

// RefreshContinuousAggregates manually refreshes the continuous aggregates
// Useful after bulk data imports or backfills
func (d *DB) RefreshContinuousAggregates(ctx context.Context) error {
	// Refresh hourly aggregate for last 30 days
	_, err := d.db.ExecContext(ctx, `
		CALL refresh_continuous_aggregate('market_prices_hourly', NOW() - INTERVAL '30 days', NOW())
	`)
	if err != nil {
		d.logger.Warn("Failed to refresh hourly aggregate", zap.Error(err))
	}

	// Refresh daily aggregate for last 90 days
	_, err = d.db.ExecContext(ctx, `
		CALL refresh_continuous_aggregate('market_prices_daily', NOW() - INTERVAL '90 days', NOW())
	`)
	if err != nil {
		d.logger.Warn("Failed to refresh daily aggregate", zap.Error(err))
	}

	return nil
}

// GetPriceHistoryBucketed retrieves price history bucketed by interval
// intervalMinutes: bucket size in minutes (e.g., 1 for 1-minute buckets, 60 for hourly)
// Aggregates data within each bucket using last value for prices and sum for volume
func (d *DB) GetPriceHistoryBucketed(ctx context.Context, itemID int, from, to time.Time, intervalMinutes int, limit int) ([]PriceSnapshot, error) {
	var snapshots []PriceSnapshot

	// Use PostgreSQL's date_trunc or generate_series for bucketing
	// We'll bucket to the nearest interval and take the last price in each bucket
	query := fmt.Sprintf(`
		WITH combined AS (
			-- Recent collector snapshots
			SELECT time, item_id, lowest_sell_price, lowest_price_volume, highest_buy_price, highest_price_volume
			FROM market_prices
			WHERE item_id = $1 AND time >= $2 AND time <= $3
			
			UNION ALL
			
			-- Historical backfilled data
			SELECT time, item_id, 
				COALESCE(lowest_sell_price, average_price) as lowest_sell_price, 
				COALESCE(trade_volume, 0)::integer as lowest_price_volume,
				COALESCE(highest_sell_price, average_price) as highest_buy_price,
				COALESCE(trade_volume, 0)::integer as highest_price_volume
			FROM market_trade_history
			WHERE item_id = $1 AND time >= $2 AND time <= $3
		),
		bucketed AS (
			SELECT 
				-- Round time to nearest interval
				date_trunc('minute', time) - 
					(EXTRACT(minute FROM time)::integer %% %d) * INTERVAL '1 minute' as bucket_time,
				item_id,
				lowest_sell_price,
				lowest_price_volume,
				highest_buy_price,
				highest_price_volume,
				time
			FROM combined
		)
		SELECT DISTINCT ON (bucket_time)
			bucket_time as time,
			item_id,
			lowest_sell_price,
			lowest_price_volume,
			highest_buy_price,
			highest_price_volume
		FROM bucketed
		ORDER BY bucket_time ASC, time DESC
		LIMIT $4
	`, intervalMinutes)

	err := d.db.SelectContext(ctx, &snapshots, query, itemID, from, to, limit)
	return snapshots, err
}

// GetLatestPrice retrieves the most recent price for an item
func (d *DB) GetLatestPrice(ctx context.Context, itemID int) (*PriceSnapshot, error) {
	var snapshot PriceSnapshot
	query := `
		SELECT time, item_id, lowest_sell_price, lowest_price_volume, highest_buy_price, highest_price_volume
		FROM market_prices
		WHERE item_id = $1
		ORDER BY time DESC
		LIMIT 1
	`
	err := d.db.GetContext(ctx, &snapshot, query, itemID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &snapshot, err
}

// GetLatestPrices retrieves the most recent prices for multiple items
func (d *DB) GetLatestPrices(ctx context.Context, itemIDs []int) ([]PriceSnapshot, error) {
	if len(itemIDs) == 0 {
		return []PriceSnapshot{}, nil
	}

	query := `
		SELECT DISTINCT ON (item_id) 
			time, item_id, lowest_sell_price, lowest_price_volume, highest_buy_price, highest_price_volume
		FROM market_prices
		WHERE item_id = ANY($1)
		ORDER BY item_id, time DESC
	`
	var snapshots []PriceSnapshot
	err := d.db.SelectContext(ctx, &snapshots, query, itemIDs)
	return snapshots, err
}

// InsertTradeHistory inserts trade history data
func (d *DB) InsertTradeHistory(ctx context.Context, history *TradeHistory) error {
	query := `
		INSERT INTO market_trade_history (time, item_id, lowest_sell_price, highest_sell_price, average_price, trade_volume)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (time, item_id) DO NOTHING
	`
	_, err := d.db.ExecContext(ctx, query,
		history.Time, history.ItemID,
		history.LowestSellPrice, history.HighestSellPrice,
		history.AveragePrice, history.TradeVolume)
	return err
}

// InsertTradeHistoryBatch inserts multiple trade history records
func (d *DB) InsertTradeHistoryBatch(ctx context.Context, histories []TradeHistory) error {
	if len(histories) == 0 {
		return nil
	}

	query := `
		INSERT INTO market_trade_history (time, item_id, lowest_sell_price, highest_sell_price, average_price, trade_volume)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (time, item_id) DO NOTHING
	`

	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PreparexContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, h := range histories {
		_, err = stmt.ExecContext(ctx, h.Time, h.ItemID, h.LowestSellPrice, h.HighestSellPrice, h.AveragePrice, h.TradeVolume)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetTradeHistory retrieves trade history for an item
func (d *DB) GetTradeHistory(ctx context.Context, itemID int, from, to time.Time, limit int) ([]TradeHistory, error) {
	var history []TradeHistory
	query := `
		SELECT time, item_id, lowest_sell_price, highest_sell_price, average_price, trade_volume
		FROM market_trade_history
		WHERE item_id = $1 AND time >= $2 AND time <= $3
		ORDER BY time DESC
		LIMIT $4
	`
	err := d.db.SelectContext(ctx, &history, query, itemID, from, to, limit)
	return history, err
}

// UpdateDailyAggregate updates or creates a daily aggregate for an item
func (d *DB) UpdateDailyAggregate(ctx context.Context, itemID int, date time.Time) error {
	query := `
		INSERT INTO market_price_daily (date, item_id, open_price, high_price, low_price, close_price, avg_price, total_sell_volume, total_buy_volume, sample_count)
		SELECT 
			$2::date as date,
			$1 as item_id,
			(SELECT lowest_sell_price FROM market_prices WHERE item_id = $1 AND time::date = $2::date ORDER BY time ASC LIMIT 1) as open_price,
			MAX(lowest_sell_price) as high_price,
			MIN(NULLIF(lowest_sell_price, 0)) as low_price,
			(SELECT lowest_sell_price FROM market_prices WHERE item_id = $1 AND time::date = $2::date ORDER BY time DESC LIMIT 1) as close_price,
			AVG(NULLIF(lowest_sell_price, 0)) as avg_price,
			SUM(lowest_price_volume) as total_sell_volume,
			SUM(highest_price_volume) as total_buy_volume,
			COUNT(*) as sample_count
		FROM market_prices
		WHERE item_id = $1 AND time::date = $2::date
		ON CONFLICT (date, item_id) DO UPDATE SET
			open_price = EXCLUDED.open_price,
			high_price = EXCLUDED.high_price,
			low_price = EXCLUDED.low_price,
			close_price = EXCLUDED.close_price,
			avg_price = EXCLUDED.avg_price,
			total_sell_volume = EXCLUDED.total_sell_volume,
			total_buy_volume = EXCLUDED.total_buy_volume,
			sample_count = EXCLUDED.sample_count
	`
	_, err := d.db.ExecContext(ctx, query, itemID, date)
	return err
}

// GetDailyAggregates retrieves daily aggregates for an item
func (d *DB) GetDailyAggregates(ctx context.Context, itemID int, from, to time.Time) ([]DailyAggregate, error) {
	var aggregates []DailyAggregate
	query := `
		SELECT date, item_id, open_price, high_price, low_price, close_price, avg_price, total_sell_volume, total_buy_volume, sample_count
		FROM market_price_daily
		WHERE item_id = $1 AND date >= $2 AND date <= $3
		ORDER BY date ASC
	`
	err := d.db.SelectContext(ctx, &aggregates, query, itemID, from, to)
	return aggregates, err
}

// GetCollectorState retrieves a collector state value
func (d *DB) GetCollectorState(ctx context.Context, key string) (string, error) {
	var value string
	err := d.db.GetContext(ctx, &value, `SELECT value FROM market_collector_state WHERE key = $1`, key)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetCollectorState sets a collector state value
func (d *DB) SetCollectorState(ctx context.Context, key, value string) error {
	query := `
		INSERT INTO market_collector_state (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`
	_, err := d.db.ExecContext(ctx, query, key, value)
	return err
}

// PriceChange represents a price change for an item
type PriceChange struct {
	ItemID        int     `db:"item_id" json:"item_id"`
	NameID        string  `db:"name_id" json:"name_id"`
	DisplayName   string  `db:"display_name" json:"display_name"`
	CurrentPrice  int     `db:"current_price" json:"current_price"`
	PreviousPrice int     `db:"previous_price" json:"previous_price"`
	PriceChange   int     `db:"price_change" json:"price_change"`
	ChangePercent float64 `db:"change_percent" json:"change_percent"`
	Volume        int     `db:"volume" json:"volume"`
}

// GetTopMovers retrieves items with the biggest price changes
// Uses the oldest available data for comparison if not enough history
func (d *DB) GetTopMovers(ctx context.Context, hours int, limit int, gainers bool) ([]PriceChange, error) {
	var results []PriceChange
	orderDir := "DESC"
	if !gainers {
		orderDir = "ASC"
	}

	// Get current prices and compare to oldest available price within the time window
	// This way we get results even with limited history
	query := fmt.Sprintf(`
		WITH current_prices AS (
			SELECT DISTINCT ON (item_id) item_id, lowest_sell_price as price, lowest_price_volume as volume, time
			FROM market_prices
			WHERE time > NOW() - INTERVAL '1 hour'
			ORDER BY item_id, time DESC
		),
		old_prices AS (
			-- Get the oldest price for each item (either from target time or oldest available)
			SELECT DISTINCT ON (item_id) item_id, lowest_sell_price as price, time
			FROM market_prices
			WHERE time < (SELECT MIN(time) FROM current_prices WHERE current_prices.item_id = market_prices.item_id)
			ORDER BY item_id, time ASC
		)
		SELECT 
			c.item_id,
			i.name_id,
			COALESCE(i.display_name, i.name_id) as display_name,
			c.price as current_price,
			o.price as previous_price,
			c.price - o.price as price_change,
			CASE WHEN o.price > 0 THEN ((c.price - o.price)::float / o.price * 100) ELSE 0 END as change_percent,
			c.volume
		FROM current_prices c
		JOIN old_prices o ON c.item_id = o.item_id
		JOIN market_items i ON c.item_id = i.id
		WHERE c.price > 0 AND o.price > 0 AND c.price != o.price
		ORDER BY change_percent %s
		LIMIT $1
	`, orderDir)

	err := d.db.SelectContext(ctx, &results, query, limit)
	return results, err
}

// GetMostTraded retrieves items with the highest trading volume
func (d *DB) GetMostTraded(ctx context.Context, hours int, limit int) ([]PriceChange, error) {
	var results []PriceChange
	query := `
		WITH recent_prices AS (
			SELECT DISTINCT ON (item_id) item_id, lowest_sell_price as price, lowest_price_volume as volume
			FROM market_prices
			WHERE time > NOW() - INTERVAL '30 minutes'
			ORDER BY item_id, time DESC
		)
		SELECT 
			r.item_id,
			i.name_id,
			COALESCE(i.display_name, i.name_id) as display_name,
			r.price as current_price,
			0 as previous_price,
			0 as price_change,
			0 as change_percent,
			r.volume
		FROM recent_prices r
		JOIN market_items i ON r.item_id = i.id
		WHERE r.volume > 0
		ORDER BY r.volume DESC
		LIMIT $1
	`
	err := d.db.SelectContext(ctx, &results, query, limit)
	return results, err
}

// GetItemCount returns the total number of items
func (d *DB) GetItemCount(ctx context.Context) (int, error) {
	var count int
	// Only count items that have price data
	err := d.db.GetContext(ctx, &count, `
		SELECT COUNT(DISTINCT item_id) FROM market_prices
	`)
	return count, err
}

// GetTotalItemCount returns the total number of items in the database (including those without market data)
func (d *DB) GetTotalItemCount(ctx context.Context) (int, error) {
	var count int
	err := d.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM market_items`)
	return count, err
}

// GetActiveItemCount returns the count of items with price data in the last hour
func (d *DB) GetActiveItemCount(ctx context.Context) (int, error) {
	var count int
	err := d.db.GetContext(ctx, &count, `
		SELECT COUNT(DISTINCT item_id) 
		FROM market_prices 
		WHERE time > NOW() - INTERVAL '1 hour'
	`)
	return count, err
}

// GetPriceSnapshotCount returns the total number of price snapshots
func (d *DB) GetPriceSnapshotCount(ctx context.Context) (int64, error) {
	var count int64
	err := d.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM market_prices`)
	return count, err
}

// CachedMarketOverview represents the cached market overview data
type CachedMarketOverview struct {
	TotalItems  int           `db:"total_items" json:"total_items"`
	ActiveItems int           `db:"active_items" json:"active_items"`
	TopGainers  []PriceChange `json:"top_gainers"`
	TopLosers   []PriceChange `json:"top_losers"`
	MostTraded  []PriceChange `json:"most_traded"`
	UpdatedAt   time.Time     `db:"updated_at" json:"last_updated"`
}

// cachedOverviewRow is the raw row from the database
type cachedOverviewRow struct {
	TotalItems  int       `db:"total_items"`
	ActiveItems int       `db:"active_items"`
	TopGainers  []byte    `db:"top_gainers"`
	TopLosers   []byte    `db:"top_losers"`
	MostTraded  []byte    `db:"most_traded"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// GetCachedMarketOverview retrieves the pre-computed market overview
func (d *DB) GetCachedMarketOverview(ctx context.Context) (*CachedMarketOverview, error) {
	var row cachedOverviewRow
	err := d.db.GetContext(ctx, &row, `
		SELECT total_items, active_items, top_gainers, top_losers, most_traded, updated_at
		FROM market_overview_cache
		WHERE id = 1
	`)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	overview := &CachedMarketOverview{
		TotalItems:  row.TotalItems,
		ActiveItems: row.ActiveItems,
		UpdatedAt:   row.UpdatedAt,
	}

	// Parse JSON arrays
	if len(row.TopGainers) > 0 {
		if err := json.Unmarshal(row.TopGainers, &overview.TopGainers); err != nil {
			d.logger.Warn("Failed to unmarshal top_gainers", zap.Error(err))
			overview.TopGainers = []PriceChange{}
		}
	}
	if len(row.TopLosers) > 0 {
		if err := json.Unmarshal(row.TopLosers, &overview.TopLosers); err != nil {
			d.logger.Warn("Failed to unmarshal top_losers", zap.Error(err))
			overview.TopLosers = []PriceChange{}
		}
	}
	if len(row.MostTraded) > 0 {
		if err := json.Unmarshal(row.MostTraded, &overview.MostTraded); err != nil {
			d.logger.Warn("Failed to unmarshal most_traded", zap.Error(err))
			overview.MostTraded = []PriceChange{}
		}
	}

	return overview, nil
}

// UpdateCachedMarketOverview updates the cached market overview
// This should be called periodically by the collector
func (d *DB) UpdateCachedMarketOverview(ctx context.Context, totalItems, activeItems int, gainers, losers, mostTraded []PriceChange) error {
	gainersJSON, err := json.Marshal(gainers)
	if err != nil {
		return fmt.Errorf("failed to marshal gainers: %w", err)
	}
	losersJSON, err := json.Marshal(losers)
	if err != nil {
		return fmt.Errorf("failed to marshal losers: %w", err)
	}
	mostTradedJSON, err := json.Marshal(mostTraded)
	if err != nil {
		return fmt.Errorf("failed to marshal most_traded: %w", err)
	}

	query := `
		INSERT INTO market_overview_cache (id, total_items, active_items, top_gainers, top_losers, most_traded, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, NOW())
		ON CONFLICT (id) DO UPDATE SET
			total_items = EXCLUDED.total_items,
			active_items = EXCLUDED.active_items,
			top_gainers = EXCLUDED.top_gainers,
			top_losers = EXCLUDED.top_losers,
			most_traded = EXCLUDED.most_traded,
			updated_at = NOW()
	`
	_, err = d.db.ExecContext(ctx, query, totalItems, activeItems, gainersJSON, losersJSON, mostTradedJSON)
	return err
}

// GetTopMoversOptimized retrieves items with the biggest price changes using an optimized query
// This is used to pre-compute the cache
func (d *DB) GetTopMoversOptimized(ctx context.Context, hours int, limit int, gainers bool) ([]PriceChange, error) {
	var results []PriceChange
	orderDir := "DESC"
	if !gainers {
		orderDir = "ASC"
	}

	// Optimized query: Use a simple time-based comparison
	// Compare latest price to the price from ~24h ago (or closest available)
	query := fmt.Sprintf(`
		WITH latest AS (
			SELECT DISTINCT ON (item_id) 
				item_id, 
				lowest_sell_price as current_price, 
				lowest_price_volume as volume,
				time as current_time
			FROM market_prices
			WHERE time > NOW() - INTERVAL '2 hours'
			  AND lowest_sell_price > 0
			ORDER BY item_id, time DESC
		),
		historical AS (
			SELECT DISTINCT ON (item_id)
				item_id,
				lowest_sell_price as previous_price
			FROM market_prices
			WHERE time BETWEEN NOW() - INTERVAL '%d hours' - INTERVAL '2 hours' 
			              AND NOW() - INTERVAL '%d hours' + INTERVAL '2 hours'
			  AND lowest_sell_price > 0
			ORDER BY item_id, time DESC
		)
		SELECT 
			l.item_id,
			i.name_id,
			COALESCE(i.display_name, i.name_id) as display_name,
			l.current_price,
			h.previous_price,
			l.current_price - h.previous_price as price_change,
			ROUND(((l.current_price - h.previous_price)::numeric / h.previous_price * 100), 2) as change_percent,
			l.volume
		FROM latest l
		JOIN historical h ON l.item_id = h.item_id
		JOIN market_items i ON l.item_id = i.id
		WHERE l.current_price != h.previous_price
		ORDER BY change_percent %s
		LIMIT $1
	`, hours, hours, orderDir)

	err := d.db.SelectContext(ctx, &results, query, limit)
	return results, err
}

// GetMostTradedOptimized retrieves items with the highest trading volume using an optimized query
func (d *DB) GetMostTradedOptimized(ctx context.Context, limit int) ([]PriceChange, error) {
	var results []PriceChange
	query := `
		SELECT DISTINCT ON (item_id)
			mp.item_id,
			i.name_id,
			COALESCE(i.display_name, i.name_id) as display_name,
			mp.lowest_sell_price as current_price,
			0 as previous_price,
			0 as price_change,
			0 as change_percent,
			mp.lowest_price_volume as volume
		FROM market_prices mp
		JOIN market_items i ON mp.item_id = i.id
		WHERE mp.time > NOW() - INTERVAL '1 hour'
		  AND mp.lowest_price_volume > 0
		ORDER BY mp.item_id, mp.time DESC
	`

	// First get distinct items, then sort by volume
	wrapperQuery := fmt.Sprintf(`
		WITH latest_volumes AS (%s)
		SELECT * FROM latest_volumes
		ORDER BY volume DESC
		LIMIT $1
	`, query)

	err := d.db.SelectContext(ctx, &results, wrapperQuery, limit)
	return results, err
}
