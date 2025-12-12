package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// IdleClans API base URL
	apiBaseURL = "https://query.idleclans.com/api"
	// Items API base URL (for item list)
	itemsAPIURL = "https://idleclans.uraxys.dev/api/items/all"
	// Default collection interval - how often we run a batch
	DefaultCollectInterval = 2 * time.Minute
	// Rate limit: API allows 40 requests per 2 minutes
	maxRequestsPer2Min = 40
	// Safety margin - use 35 to leave buffer
	safeRequestsPer2Min = 35
)

// Collector fetches price data from the IdleClans API
type Collector struct {
	db        *DB
	analytics *Analytics
	logger    *zap.Logger
	client    *http.Client
	interval  time.Duration
	stopCh    chan struct{}
	wg        sync.WaitGroup

	// Rate limiting - requests per 2 minute window
	batchSize int

	// Backoff state for 429 responses
	backoffMu       sync.RWMutex
	backoffUntil    time.Time
	backoffDuration time.Duration

	// Item tracking
	itemsMu          sync.RWMutex
	items            map[int]string // id -> name_id
	itemsLastRefresh time.Time      // when items were last fetched
}

// CollectorConfig holds configuration for the collector
type CollectorConfig struct {
	Interval  time.Duration
	BatchSize int // requests per batch (respecting rate limit)
}

// NewCollector creates a new price collector
func NewCollector(db *DB, logger *zap.Logger, config *CollectorConfig) *Collector {
	if config == nil {
		config = &CollectorConfig{
			Interval:  DefaultCollectInterval,
			BatchSize: safeRequestsPer2Min,
		}
	}
	if config.Interval == 0 {
		config.Interval = DefaultCollectInterval
	}
	if config.BatchSize == 0 {
		config.BatchSize = safeRequestsPer2Min
	}

	return &Collector{
		db:        db,
		analytics: NewAnalytics(db),
		logger:    logger,
		client:    &http.Client{Timeout: 30 * time.Second},
		interval:  config.Interval,
		stopCh:    make(chan struct{}),
		batchSize: config.BatchSize,
		items:     make(map[int]string),
	}
}

// Start begins the collection loop
func (c *Collector) Start(ctx context.Context) {
	c.wg.Add(1)
	go c.run(ctx)
}

// Stop gracefully stops the collector
func (c *Collector) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

func (c *Collector) run(ctx context.Context) {
	defer c.wg.Done()

	c.logger.Info("Starting market price collector",
		zap.Duration("bulk_interval", 1*time.Minute),
		zap.Duration("history_interval", c.interval),
		zap.Int("batch_size", c.batchSize))

	// Refresh item list first
	if err := c.refreshItems(ctx); err != nil {
		c.logger.Error("Failed to refresh items on startup", zap.Error(err))
	}

	// Initial collection
	c.collectBulkPrices(ctx)
	c.collectHistory(ctx)

	// Bulk prices every 1 minute (single API call)
	bulkTicker := time.NewTicker(1 * time.Minute)
	defer bulkTicker.Stop()

	// History backfills every 2 minutes (multiple API calls, rate limited)
	historyTicker := time.NewTicker(c.interval)
	defer historyTicker.Stop()

	// Daily full backfill - runs once per day at ~3 AM UTC
	// Calculate time until next 3 AM UTC
	now := time.Now().UTC()
	next3AM := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, time.UTC)
	if now.After(next3AM) {
		next3AM = next3AM.Add(24 * time.Hour)
	}
	dailyTimer := time.NewTimer(next3AM.Sub(now))
	defer dailyTimer.Stop()

	c.logger.Info("Daily backfill scheduled", zap.Time("next_run", next3AM))

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Collector stopped due to context cancellation")
			return
		case <-c.stopCh:
			c.logger.Info("Collector stopped")
			return
		case <-bulkTicker.C:
			c.collectBulkPrices(ctx)
		case <-historyTicker.C:
			c.collectHistory(ctx)
		case <-dailyTimer.C:
			c.runDailyBackfill(ctx)
			// Reset timer for next day
			dailyTimer.Reset(24 * time.Hour)
		}
	}
}

// runDailyBackfill runs a full history backfill and refreshes aggregates
func (c *Collector) runDailyBackfill(ctx context.Context) {
	c.logger.Info("Starting daily backfill...")
	start := time.Now()

	// Reset history_backfilled flag for all items so they get refreshed
	_, err := c.db.db.ExecContext(ctx, `UPDATE market_items SET history_backfilled = FALSE`)
	if err != nil {
		c.logger.Error("Failed to reset backfill flags", zap.Error(err))
		return
	}

	// Run backfill in background (don't block the main loop)
	go func() {
		bgCtx := context.Background()
		if err := c.BackfillAllHistory(bgCtx); err != nil {
			c.logger.Error("Daily backfill failed", zap.Error(err))
		} else {
			// Refresh continuous aggregates after backfill
			if err := c.db.RefreshContinuousAggregates(bgCtx); err != nil {
				c.logger.Warn("Failed to refresh continuous aggregates", zap.Error(err))
			}
			c.logger.Info("Daily backfill complete", zap.Duration("duration", time.Since(start)))
		}
	}()
}

// collectBulkPrices fetches all latest prices in a single API call (runs every minute)
func (c *Collector) collectBulkPrices(ctx context.Context) {
	start := time.Now()

	// Refresh item list periodically (once per hour)
	if err := c.refreshItems(ctx); err != nil {
		c.logger.Error("Failed to refresh items", zap.Error(err))
		// Continue anyway - we might have cached items
	}

	// Fetch ALL latest prices in bulk (1 API call)
	// This gives us current orderbook state for every item
	bulkCollected, bulkErr := c.collectAllLatestPrices(ctx)
	if bulkErr != nil {
		c.logger.Error("Failed to collect bulk latest prices", zap.Error(bulkErr))
		return
	}

	duration := time.Since(start)
	c.logger.Info("Bulk price collection complete",
		zap.Int("items_collected", bulkCollected),
		zap.Duration("duration", duration))

	// Save collection stats
	_ = c.db.SetCollectorState(ctx, "last_collection_time", time.Now().UTC().Format(time.RFC3339))
	_ = c.db.SetCollectorState(ctx, "last_collection_count", strconv.Itoa(bulkCollected))

	// Refresh the market overview cache (pre-compute gainers/losers/most traded)
	// This runs in the background so it doesn't block the next collection
	go func() {
		cacheStart := time.Now()
		if err := c.analytics.RefreshMarketOverviewCache(ctx); err != nil {
			c.logger.Warn("Failed to refresh market overview cache", zap.Error(err))
		} else {
			c.logger.Debug("Refreshed market overview cache", zap.Duration("duration", time.Since(cacheStart)))
		}
	}()
}

// collectHistory backfills historical data for items (runs every 2 minutes)
func (c *Collector) collectHistory(ctx context.Context) {
	start := time.Now()

	// Use batch size for history backfills
	historyBackfillBatch := c.batchSize
	if historyBackfillBatch < 1 {
		historyBackfillBatch = 1
	}

	// Backfill history for items that need it
	historyBackfilled, historyErrors := c.backfillHistoryBatch(ctx, historyBackfillBatch)

	if historyBackfilled > 0 || historyErrors > 0 {
		duration := time.Since(start)
		c.logger.Info("History backfill complete",
			zap.Int("items_backfilled", historyBackfilled),
			zap.Int("errors", historyErrors),
			zap.Duration("duration", duration))
	}
}

// collectPricesBatch collects prices for items using the history endpoint
// This gives us multiple data points per API call, filling in gaps
func (c *Collector) collectPricesBatch(ctx context.Context, batchSize int) (int, int) {
	// Get items that need price updates (oldest first)
	items, err := c.db.GetItemsNeedingPriceUpdate(ctx, batchSize)
	if err != nil {
		c.logger.Error("Failed to get items needing update", zap.Error(err))
		return 0, 0
	}

	if len(items) == 0 {
		return 0, 0
	}

	c.logger.Debug("Processing price update batch", zap.Int("items", len(items)))

	collected := 0
	totalRecords := 0
	errors := 0

	// Calculate delay to spread requests evenly over 2 minutes
	delay := (2 * time.Minute) / time.Duration(c.batchSize)

	for i, item := range items {
		select {
		case <-ctx.Done():
			return collected, errors
		case <-c.stopCh:
			return collected, errors
		default:
		}

		// Rate limit - sleep between requests (but not before the first one)
		if i > 0 {
			time.Sleep(delay)
		}

		// Use the history endpoint - it gives us multiple data points per call
		history, err := c.FetchPriceHistory(ctx, item.ID)
		if err != nil {
			c.logger.Debug("Failed to fetch price history",
				zap.Int("item_id", item.ID),
				zap.String("item_name", item.NameID),
				zap.Error(err))
			errors++
			// Still update timestamp to avoid retrying failed items forever
			_ = c.db.UpdateItemPriceCollected(ctx, item.ID)
			continue
		}

		// Skip items with no history
		if len(history) == 0 {
			_ = c.db.UpdateItemPriceCollected(ctx, item.ID)
			continue
		}

		// Convert history entries to trade history records and insert
		trades := make([]TradeHistory, 0, len(history))
		for _, h := range history {
			trades = append(trades, TradeHistory{
				Time:             h.Timestamp,
				ItemID:           item.ID,
				LowestSellPrice:  h.LowestSellPrice,
				HighestSellPrice: h.HighestSellPrice,
				AveragePrice:     h.AveragePrice,
				TradeVolume:      h.TradeVolume,
			})
		}

		if err := c.db.InsertTradeHistoryBatch(ctx, trades); err != nil {
			c.logger.Error("Failed to insert trade history",
				zap.Int("item_id", item.ID),
				zap.Error(err))
			errors++
			continue
		}

		// Also insert the most recent price as a current snapshot
		latestEntry := history[len(history)-1]
		if latestEntry.AveragePrice > 0 || latestEntry.LowestSellPrice > 0 {
			snapshot := PriceSnapshot{
				Time:               latestEntry.Timestamp,
				ItemID:             item.ID,
				LowestSellPrice:    latestEntry.LowestSellPrice,
				LowestPriceVolume:  0,
				HighestBuyPrice:    latestEntry.AveragePrice,
				HighestPriceVolume: 0,
			}
			_ = c.db.InsertPriceSnapshot(ctx, &snapshot)
		}

		c.logger.Info("Collected price history",
			zap.Int("item_id", item.ID),
			zap.String("item_name", item.NameID),
			zap.Int("records", len(history)))

		_ = c.db.UpdateItemPriceCollected(ctx, item.ID)
		collected++
		totalRecords += len(history)
	}

	if totalRecords > 0 {
		c.logger.Info("Price collection summary",
			zap.Int("items_updated", collected),
			zap.Int("total_records", totalRecords),
			zap.Int("errors", errors))
	}

	return collected, errors
}

// collectLatestPricesBatch collects current prices using the latest endpoint (faster, for real-time updates)
func (c *Collector) collectLatestPricesBatch(ctx context.Context, batchSize int) (int, int) {
	items, err := c.db.GetItemsNeedingPriceUpdate(ctx, batchSize)
	if err != nil {
		c.logger.Error("Failed to get items needing update", zap.Error(err))
		return 0, 0
	}

	if len(items) == 0 {
		return 0, 0
	}

	collected := 0
	errors := 0
	now := time.Now().UTC().Truncate(time.Minute)
	delay := (2 * time.Minute) / time.Duration(c.batchSize)

	for i, item := range items {
		select {
		case <-ctx.Done():
			return collected, errors
		case <-c.stopCh:
			return collected, errors
		default:
		}

		if i > 0 {
			time.Sleep(delay)
		}

		price, err := c.fetchLatestPrice(ctx, item.ID)
		if err != nil {
			errors++
			continue
		}

		if price.LowestSellPrice == 0 && price.HighestBuyPrice == 0 {
			_ = c.db.UpdateItemPriceCollected(ctx, item.ID)
			continue
		}

		snapshot := PriceSnapshot{
			Time:               now,
			ItemID:             item.ID,
			LowestSellPrice:    price.LowestSellPrice,
			LowestPriceVolume:  price.LowestPriceVolume,
			HighestBuyPrice:    price.HighestBuyPrice,
			HighestPriceVolume: price.HighestPriceVolume,
		}

		if err := c.db.InsertPriceSnapshot(ctx, &snapshot); err != nil {
			c.logger.Error("Failed to insert price snapshot", zap.Error(err))
			errors++
			continue
		}

		// Update the item's last collected timestamp
		_ = c.db.UpdateItemPriceCollected(ctx, item.ID)
		collected++
	}

	return collected, errors
}

// backfillHistoryBatch backfills history for items that need it
// Note: Each item requires 4 API calls (one per period: 1d, 7d, 30d, 1y)
func (c *Collector) backfillHistoryBatch(ctx context.Context, batchSize int) (int, int) {
	// Each item needs 4 API calls for all periods
	// So we can only backfill batchSize/4 items per batch
	itemsToBackfill := batchSize / 4
	if itemsToBackfill < 1 {
		itemsToBackfill = 1
	}

	// Get items that need history backfill
	items, err := c.db.GetItemsNeedingHistoryBackfill(ctx, itemsToBackfill)
	if err != nil {
		c.logger.Error("Failed to get items needing backfill", zap.Error(err))
		return 0, 0
	}

	if len(items) == 0 {
		return 0, 0
	}

	c.logger.Debug("Processing history backfill batch",
		zap.Int("items", len(items)),
		zap.Int("api_calls", len(items)*4))

	backfilled := 0
	errors := 0

	// Calculate delay between API calls to spread over 2 minutes
	// Total calls = items * 4 periods
	totalCalls := len(items) * 4
	delay := (2 * time.Minute) / time.Duration(totalCalls+1)

	for _, item := range items {
		select {
		case <-ctx.Done():
			return backfilled, errors
		case <-c.stopCh:
			return backfilled, errors
		default:
		}

		count, err := c.BackfillHistory(ctx, item.ID)
		if err != nil {
			c.logger.Debug("Failed to backfill history",
				zap.Int("item_id", item.ID),
				zap.String("item_name", item.NameID),
				zap.Error(err))
			errors++
			// Still mark as backfilled to avoid retrying 404s forever
			_ = c.db.MarkItemHistoryBackfilled(ctx, item.ID)
			continue
		}

		if count > 0 {
			c.logger.Info("Backfilled history",
				zap.Int("item_id", item.ID),
				zap.String("item_name", item.NameID),
				zap.Int("records", count))
		}

		_ = c.db.MarkItemHistoryBackfilled(ctx, item.ID)
		backfilled++

		// Rate limit delay between items (not needed between periods - BackfillHistory handles that)
		time.Sleep(delay * 4) // Account for the 4 API calls just made
	}

	return backfilled, errors
}

// ItemInfo represents item info from the items API
type ItemInfo struct {
	NameID     string `json:"name_id"`
	InternalID int    `json:"internal_id"`
}

func (c *Collector) refreshItems(ctx context.Context) error {
	return c.refreshItemsWithForce(ctx, false)
}

func (c *Collector) refreshItemsWithForce(ctx context.Context, force bool) error {
	c.itemsMu.RLock()
	itemCount := len(c.items)
	lastRefresh := c.itemsLastRefresh
	c.itemsMu.RUnlock()

	// Only refresh once per hour unless forced or no items loaded yet
	if !force && itemCount > 0 && time.Since(lastRefresh) < time.Hour {
		c.logger.Debug("Skipping item refresh, last refresh was recent",
			zap.Duration("since_last_refresh", time.Since(lastRefresh)),
			zap.Int("cached_items", itemCount))
		return nil
	}

	c.logger.Info("Refreshing item list from API", zap.String("url", itemsAPIURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, itemsAPIURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "idleclans-bot/go")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch items: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("items API returned status %d", resp.StatusCode)
	}

	var items []ItemInfo
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return fmt.Errorf("failed to decode items: %w", err)
	}

	c.logger.Info("Fetched items from API", zap.Int("count", len(items)))

	c.itemsMu.Lock()
	c.items = make(map[int]string, len(items))
	for _, item := range items {
		c.items[item.InternalID] = item.NameID
	}
	c.itemsLastRefresh = time.Now()
	c.itemsMu.Unlock()

	// Upsert items into database
	upserted := 0
	for _, item := range items {
		displayName := formatDisplayName(item.NameID)
		category := guessCategory(item.NameID)
		if err := c.db.UpsertItem(ctx, item.InternalID, item.NameID, displayName, category); err != nil {
			c.logger.Warn("Failed to upsert item",
				zap.Int("id", item.InternalID),
				zap.String("name", item.NameID),
				zap.Error(err))
		} else {
			upserted++
		}
	}

	c.logger.Info("Item list refreshed", zap.Int("fetched", len(items)), zap.Int("upserted", upserted))
	return nil
}

// LatestPrice represents the response from the latest price API
type LatestPrice struct {
	ItemID             int `json:"itemId"`
	LowestSellPrice    int `json:"lowestSellPrice"`
	LowestPriceVolume  int `json:"lowestPriceVolume"`
	HighestBuyPrice    int `json:"highestBuyPrice"`
	HighestPriceVolume int `json:"highestPriceVolume"`
}

// fetchAllLatestPrices fetches current prices for ALL items in a single API call
// This is much more efficient than individual requests
func (c *Collector) fetchAllLatestPrices(ctx context.Context) ([]LatestPrice, error) {
	// Check if we're in backoff
	if err := c.waitForBackoff(ctx); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/PlayerMarket/items/prices/latest", apiBaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "idleclans-bot/go")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Handle 429 Too Many Requests
	if resp.StatusCode == http.StatusTooManyRequests {
		var retryAfter time.Duration
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if seconds, err := strconv.Atoi(ra); err == nil {
				retryAfter = time.Duration(seconds) * time.Second
			}
		}
		c.setBackoff(retryAfter)
		return nil, fmt.Errorf("rate limited (429)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var prices []LatestPrice
	if err := json.NewDecoder(resp.Body).Decode(&prices); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return prices, nil
}

// collectAllLatestPrices fetches and stores current prices for all items in bulk
// Returns the number of items updated and any error
func (c *Collector) collectAllLatestPrices(ctx context.Context) (int, error) {
	c.logger.Info("Fetching all latest prices in bulk...")

	prices, err := c.fetchAllLatestPrices(ctx)
	if err != nil {
		return 0, err
	}

	if len(prices) == 0 {
		c.logger.Warn("No prices returned from bulk API")
		return 0, nil
	}

	now := time.Now().UTC()
	inserted := 0

	for _, price := range prices {
		// Skip items with no market activity
		if price.LowestSellPrice == 0 && price.HighestBuyPrice == 0 {
			continue
		}

		snapshot := PriceSnapshot{
			Time:               now,
			ItemID:             price.ItemID,
			LowestSellPrice:    price.LowestSellPrice,
			LowestPriceVolume:  price.LowestPriceVolume,
			HighestBuyPrice:    price.HighestBuyPrice,
			HighestPriceVolume: price.HighestPriceVolume,
		}

		if err := c.db.InsertPriceSnapshot(ctx, &snapshot); err != nil {
			c.logger.Debug("Failed to insert bulk price snapshot",
				zap.Int("item_id", price.ItemID),
				zap.Error(err))
			continue
		}

		// Update the item's last collected timestamp
		_ = c.db.UpdateItemPriceCollected(ctx, price.ItemID)
		inserted++
	}

	c.logger.Info("Bulk price collection complete",
		zap.Int("total_items", len(prices)),
		zap.Int("inserted", inserted))

	return inserted, nil
}

// checkBackoff returns true if we should wait due to rate limiting
func (c *Collector) checkBackoff() bool {
	c.backoffMu.RLock()
	defer c.backoffMu.RUnlock()
	return time.Now().Before(c.backoffUntil)
}

// getBackoffRemaining returns how long we need to wait
func (c *Collector) getBackoffRemaining() time.Duration {
	c.backoffMu.RLock()
	defer c.backoffMu.RUnlock()
	remaining := time.Until(c.backoffUntil)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// setBackoff sets a backoff period after receiving a 429
func (c *Collector) setBackoff(retryAfter time.Duration) {
	c.backoffMu.Lock()
	defer c.backoffMu.Unlock()

	// Use Retry-After if provided, otherwise exponential backoff
	if retryAfter > 0 {
		c.backoffDuration = retryAfter
	} else {
		// Exponential backoff: start at 30s, double each time, max 5 minutes
		if c.backoffDuration == 0 {
			c.backoffDuration = 30 * time.Second
		} else {
			c.backoffDuration *= 2
			if c.backoffDuration > 5*time.Minute {
				c.backoffDuration = 5 * time.Minute
			}
		}
	}

	c.backoffUntil = time.Now().Add(c.backoffDuration)
	c.logger.Warn("Rate limited (429), backing off",
		zap.Duration("backoff_duration", c.backoffDuration),
		zap.Time("backoff_until", c.backoffUntil))
}

// clearBackoff resets the backoff state after a successful request
func (c *Collector) clearBackoff() {
	c.backoffMu.Lock()
	defer c.backoffMu.Unlock()
	c.backoffDuration = 0
}

// waitForBackoff waits until the backoff period is over
func (c *Collector) waitForBackoff(ctx context.Context) error {
	remaining := c.getBackoffRemaining()
	if remaining <= 0 {
		return nil
	}

	c.logger.Info("Waiting for rate limit backoff", zap.Duration("remaining", remaining))

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.stopCh:
		return fmt.Errorf("collector stopped")
	case <-time.After(remaining):
		return nil
	}
}

func (c *Collector) fetchLatestPrice(ctx context.Context, itemID int) (*LatestPrice, error) {
	// Check if we're in backoff
	if err := c.waitForBackoff(ctx); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/PlayerMarket/items/prices/latest/%d", apiBaseURL, itemID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "idleclans-bot/go")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Handle 429 Too Many Requests
	if resp.StatusCode == http.StatusTooManyRequests {
		// Check for Retry-After header
		var retryAfter time.Duration
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if seconds, err := strconv.Atoi(ra); err == nil {
				retryAfter = time.Duration(seconds) * time.Second
			}
		}
		c.setBackoff(retryAfter)
		return nil, fmt.Errorf("rate limited (429)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Successful request - clear backoff
	c.clearBackoff()

	var price LatestPrice
	if err := json.NewDecoder(resp.Body).Decode(&price); err != nil {
		return nil, err
	}

	return &price, nil
}

// PriceHistoryEntry represents an entry from the price history API
type PriceHistoryEntry struct {
	ItemID           int       `json:"itemId"`
	Timestamp        time.Time `json:"timestamp"`
	LowestSellPrice  int       `json:"lowesSellPrice"` // Note: typo in API
	HighestSellPrice int       `json:"highestSellPrice"`
	AveragePrice     int       `json:"averagePrice"`
	TradeVolume      float64   `json:"tradeVolume"`
}

// FetchPriceHistory fetches historical price data for an item from the API
// FetchPriceHistoryPeriod fetches price history for a specific period
// period can be: "1d", "7d", "30d", "1y"
func (c *Collector) FetchPriceHistoryPeriod(ctx context.Context, itemID int, period string) ([]PriceHistoryEntry, error) {
	// Check if we're in backoff
	if err := c.waitForBackoff(ctx); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/PlayerMarket/items/prices/history/%d?period=%s", apiBaseURL, itemID, period)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "idleclans-bot/go")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Handle 429 Too Many Requests
	if resp.StatusCode == http.StatusTooManyRequests {
		var retryAfter time.Duration
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if seconds, err := strconv.Atoi(ra); err == nil {
				retryAfter = time.Duration(seconds) * time.Second
			}
		}
		c.setBackoff(retryAfter)
		return nil, fmt.Errorf("rate limited (429)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Successful request - clear backoff
	c.clearBackoff()

	var history []PriceHistoryEntry
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		return nil, err
	}

	return history, nil
}

// FetchPriceHistory fetches price history using the default period (30d)
// For backwards compatibility
func (c *Collector) FetchPriceHistory(ctx context.Context, itemID int) ([]PriceHistoryEntry, error) {
	return c.FetchPriceHistoryPeriod(ctx, itemID, "30d")
}

// BackfillHistory fetches and stores historical price data for an item
// Fetches all period granularities: 1d (hourly), 7d, 30d, 1y
// Returns the total number of records inserted and any error
func (c *Collector) BackfillHistory(ctx context.Context, itemID int) (int, error) {
	// Periods from finest to coarsest granularity
	// Each period provides different data point density
	periods := []string{"1d", "7d", "30d", "1y"}

	allTrades := make([]TradeHistory, 0)
	seenTimes := make(map[int64]bool) // Deduplicate by timestamp
	periodCounts := make(map[string]int)

	for i, period := range periods {
		// Check for cancellation between periods
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-c.stopCh:
			return 0, fmt.Errorf("collector stopped")
		default:
		}

		// Small delay between API calls to avoid rate limiting
		if i > 0 {
			time.Sleep(500 * time.Millisecond)
		}

		history, err := c.FetchPriceHistoryPeriod(ctx, itemID, period)
		if err != nil {
			c.logger.Debug("Failed to fetch history period",
				zap.Int("item_id", itemID),
				zap.String("period", period),
				zap.Error(err))
			// Continue with other periods even if one fails
			continue
		}

		newRecords := 0
		for _, h := range history {
			// Deduplicate - use unix timestamp as key
			ts := h.Timestamp.Unix()
			if seenTimes[ts] {
				continue
			}
			seenTimes[ts] = true
			newRecords++

			allTrades = append(allTrades, TradeHistory{
				Time:             h.Timestamp,
				ItemID:           itemID,
				LowestSellPrice:  h.LowestSellPrice,
				HighestSellPrice: h.HighestSellPrice,
				AveragePrice:     h.AveragePrice,
				TradeVolume:      h.TradeVolume,
			})
		}
		periodCounts[period] = newRecords
	}

	if len(allTrades) == 0 {
		return 0, nil
	}

	c.logger.Info("Backfill records by period",
		zap.Int("item_id", itemID),
		zap.Int("1d", periodCounts["1d"]),
		zap.Int("7d", periodCounts["7d"]),
		zap.Int("30d", periodCounts["30d"]),
		zap.Int("1y", periodCounts["1y"]),
		zap.Int("total", len(allTrades)))

	return len(allTrades), c.db.InsertTradeHistoryBatch(ctx, allTrades)
}

// BackfillAllHistory fetches and stores historical price data for all items
// This is for manual triggering - the regular collector does incremental backfill
func (c *Collector) BackfillAllHistory(ctx context.Context) error {
	c.logger.Info("Starting historical data backfill for all items")
	start := time.Now()

	// Make sure we have the item list
	if err := c.refreshItemsWithForce(ctx, true); err != nil {
		return fmt.Errorf("failed to refresh items: %w", err)
	}

	// Get all items that need backfilling
	items, err := c.db.GetItemsNeedingHistoryBackfill(ctx, 10000)
	if err != nil {
		return fmt.Errorf("failed to get items: %w", err)
	}

	c.logger.Info("Backfilling history for items",
		zap.Int("total_items", len(items)),
		zap.Int("api_calls_needed", len(items)*4)) // 4 periods per item

	// Rate limiter - 40 requests per 2 minutes
	// Each item needs 4 API calls (one per period: 1d, 7d, 30d, 1y)
	// So delay between items should be 4x the per-request delay
	delayPerRequest := (2 * time.Minute) / time.Duration(safeRequestsPer2Min)
	delayPerItem := delayPerRequest * 4

	backfilled := 0
	skipped := 0
	errors := 0
	totalRecords := 0

	for i, item := range items {
		// Check for cancellation
		select {
		case <-ctx.Done():
			c.logger.Info("Backfill cancelled",
				zap.Int("backfilled", backfilled),
				zap.Int("remaining", len(items)-i))
			return ctx.Err()
		case <-c.stopCh:
			c.logger.Info("Backfill stopped",
				zap.Int("backfilled", backfilled),
				zap.Int("remaining", len(items)-i))
			return nil
		default:
		}

		// Rate limit - delay after each item to account for 4 API calls made
		if i > 0 {
			time.Sleep(delayPerItem)
		}

		count, err := c.BackfillHistory(ctx, item.ID)
		if err != nil {
			c.logger.Debug("Failed to backfill history",
				zap.Int("item_id", item.ID),
				zap.String("item_name", item.NameID),
				zap.Error(err))
			errors++
			// Mark as backfilled anyway to avoid infinite retries
			_ = c.db.MarkItemHistoryBackfilled(ctx, item.ID)
			continue
		}

		// Mark item as backfilled
		_ = c.db.MarkItemHistoryBackfilled(ctx, item.ID)

		if count == 0 {
			skipped++
		} else {
			backfilled++
			totalRecords += count
			c.logger.Info("Backfilled history",
				zap.Int("item_id", item.ID),
				zap.String("item_name", item.NameID),
				zap.Int("records", count))
		}

		// Progress logging every 10 items (reduced since each takes longer)
		if (i+1)%10 == 0 {
			c.logger.Info("Backfill progress",
				zap.Int("processed", i+1),
				zap.Int("total", len(items)),
				zap.Int("backfilled", backfilled),
				zap.Int("skipped", skipped),
				zap.Int("errors", errors),
				zap.Int("total_records", totalRecords))
		}
	}

	duration := time.Since(start)
	c.logger.Info("Historical backfill complete",
		zap.Int("items_backfilled", backfilled),
		zap.Int("items_skipped", skipped),
		zap.Int("errors", errors),
		zap.Int("total_records", totalRecords),
		zap.Duration("duration", duration))

	// Mark backfill as complete
	_ = c.db.SetCollectorState(ctx, "backfill_complete", time.Now().UTC().Format(time.RFC3339))
	_ = c.db.SetCollectorState(ctx, "backfill_records", strconv.Itoa(totalRecords))

	return nil
}

// NeedsBackfill checks if there are items needing history backfill
func (c *Collector) NeedsBackfill(ctx context.Context) bool {
	items, err := c.db.GetItemsNeedingHistoryBackfill(ctx, 1)
	if err != nil {
		return false
	}
	return len(items) > 0
}

func (c *Collector) updateDailyAggregates(ctx context.Context, itemCount int) {
	if itemCount == 0 {
		return
	}

	// Update aggregates for today
	today := time.Now().UTC().Truncate(24 * time.Hour)

	c.itemsMu.RLock()
	itemIDs := make([]int, 0, len(c.items))
	for id := range c.items {
		itemIDs = append(itemIDs, id)
	}
	c.itemsMu.RUnlock()

	updated := 0
	for _, itemID := range itemIDs {
		if err := c.db.UpdateDailyAggregate(ctx, itemID, today); err != nil {
			c.logger.Debug("Failed to update daily aggregate",
				zap.Int("item_id", itemID),
				zap.Error(err))
		} else {
			updated++
		}
	}

	c.logger.Debug("Updated daily aggregates", zap.Int("count", updated))
}

// GetCollectionStats returns current collection statistics
func (c *Collector) GetCollectionStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	lastTime, _ := c.db.GetCollectorState(ctx, "last_collection_time")
	lastCount, _ := c.db.GetCollectorState(ctx, "last_collection_count")

	activeItemCount, _ := c.db.GetItemCount(ctx)     // Items with market data
	totalItemCount, _ := c.db.GetTotalItemCount(ctx) // All items in DB
	snapshotCount, _ := c.db.GetPriceSnapshotCount(ctx)

	stats["last_collection_time"] = lastTime
	stats["last_collection_count"] = lastCount
	stats["active_items"] = activeItemCount // Items with price data
	stats["total_items"] = totalItemCount   // All known items
	stats["total_snapshots"] = snapshotCount
	stats["collection_interval"] = c.interval.String()

	return stats, nil
}

// Helper functions

func formatDisplayName(nameID string) string {
	// Convert snake_case to Title Case
	result := ""
	capitalize := true
	for _, c := range nameID {
		if c == '_' {
			result += " "
			capitalize = true
		} else if capitalize {
			result += string(c - 32) // ASCII uppercase
			capitalize = false
		} else {
			result += string(c)
		}
	}
	return result
}

func guessCategory(nameID string) string {
	// Simple category guessing based on name patterns
	switch {
	case contains(nameID, "_log"):
		return "logs"
	case contains(nameID, "_ore"):
		return "ores"
	case contains(nameID, "_bar"):
		return "bars"
	case contains(nameID, "raw_"):
		return "raw_fish"
	case contains(nameID, "cooked_"):
		return "cooked_fish"
	case contains(nameID, "_sword"), contains(nameID, "_axe"), contains(nameID, "_pickaxe"):
		return "tools"
	case contains(nameID, "_helm"), contains(nameID, "_body"), contains(nameID, "_legs"), contains(nameID, "_boots"), contains(nameID, "_gloves"):
		return "armor"
	case contains(nameID, "_key"):
		return "keys"
	case contains(nameID, "_potion"):
		return "potions"
	case contains(nameID, "_seed"):
		return "seeds"
	case contains(nameID, "_herb"):
		return "herbs"
	default:
		return "other"
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 1; i < len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
