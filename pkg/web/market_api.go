package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jirwin/idleclans/pkg/market"
	"go.uber.org/zap"
)

// MarketAPI handles market-related API endpoints
type MarketAPI struct {
	db                 *market.DB
	analytics          *market.Analytics
	collector          *market.Collector
	logger             *zap.Logger
	dataChangeNotifier func(changeType string)
}

// NewMarketAPI creates a new market API handler
func NewMarketAPI(db *market.DB, collector *market.Collector, logger *zap.Logger) *MarketAPI {
	return &MarketAPI{
		db:        db,
		analytics: market.NewAnalytics(db),
		collector: collector,
		logger:    logger,
	}
}

// SetDataChangeNotifier sets the callback for SSE notifications
func (m *MarketAPI) SetDataChangeNotifier(notifier func(changeType string)) {
	m.dataChangeNotifier = notifier
}

// handleMarketOverview returns a market overview with top movers
func (m *MarketAPI) handleMarketOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	overview, err := m.analytics.GetMarketOverview(ctx)
	if err != nil {
		m.logger.Error("Failed to get market overview", zap.Error(err))
		http.Error(w, "Failed to get market overview", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(overview)
}

// handleMarketStats returns collector statistics
func (m *MarketAPI) handleMarketStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, err := m.collector.GetCollectionStats(ctx)
	if err != nil {
		m.logger.Error("Failed to get collection stats", zap.Error(err))
		http.Error(w, "Failed to get stats", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleSearchItems searches for items by name
func (m *MarketAPI) handleSearchItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Missing query parameter 'q'", http.StatusBadRequest)
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	items, err := m.db.SearchItems(ctx, query, limit)
	if err != nil {
		m.logger.Error("Failed to search items", zap.Error(err), zap.String("query", query))
		http.Error(w, "Failed to search items", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// handleGetAllItems returns all items with their latest prices
// Supports optional pagination with ?page=N&limit=M query parameters
func (m *MarketAPI) handleGetAllItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check for pagination params
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")

	// If pagination params are provided, use paginated query
	if pageStr != "" || limitStr != "" {
		page := 0
		if p, err := strconv.Atoi(pageStr); err == nil && p >= 0 {
			page = p
		}

		limit := 25
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}

		offset := page * limit
		result, err := m.db.GetAllItemsWithPricesPaginated(ctx, offset, limit)
		if err != nil {
			m.logger.Error("Failed to get paginated items", zap.Error(err))
			http.Error(w, "Failed to get items", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
		return
	}

	// Fall back to fetching all items for backwards compatibility
	items, err := m.db.GetAllItemsWithPrices(ctx)
	if err != nil {
		m.logger.Error("Failed to get all items", zap.Error(err))
		http.Error(w, "Failed to get items", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// handleGetItem returns a single item with current price
func (m *MarketAPI) handleGetItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	itemIDStr := r.PathValue("itemId")
	itemID, err := strconv.Atoi(itemIDStr)
	if err != nil {
		http.Error(w, "Invalid item ID", http.StatusBadRequest)
		return
	}

	summary, err := m.analytics.GetItemSummary(ctx, itemID)
	if err != nil {
		m.logger.Error("Failed to get item summary", zap.Error(err), zap.Int("item_id", itemID))
		http.Error(w, "Failed to get item", http.StatusInternalServerError)
		return
	}

	if summary == nil || summary.Item == nil {
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	// Check if we need to refresh trade volume data
	// Only fetch if cache is missing or older than 1 hour
	m.refreshTradeVolumeIfNeeded(ctx, itemID, summary)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// refreshTradeVolumeIfNeeded fetches trade volume from the comprehensive API if cache is stale
func (m *MarketAPI) refreshTradeVolumeIfNeeded(ctx context.Context, itemID int, summary *market.ItemSummary) {
	// Check if we have cached data that's less than 1 hour old
	cache, err := m.db.GetTradeVolumeCache(ctx, itemID)
	if err == nil && cache != nil && time.Since(cache.FetchedAt) < time.Hour {
		// Cache is fresh, data already populated by GetItemSummary
		return
	}

	// Fetch fresh data from comprehensive API (in background to not block response)
	go func() {
		bgCtx := context.Background()
		
		comprehensive, err := m.collector.FetchComprehensivePrice(bgCtx, itemID)
		if err != nil {
			m.logger.Debug("Failed to fetch comprehensive price data",
				zap.Int("item_id", itemID),
				zap.Error(err))
			return
		}

		// Save to cache
		tradeVol := comprehensive.TradeVolume1Day
		avgPrice1d := comprehensive.AveragePrice1Day
		avgPrice7d := comprehensive.AveragePrice7Days
		avgPrice30d := comprehensive.AveragePrice30Days
		
		newCache := &market.TradeVolumeCache{
			ItemID:        itemID,
			TradeVolume1d: &tradeVol,
			AvgPrice1d:    &avgPrice1d,
			AvgPrice7d:    &avgPrice7d,
			AvgPrice30d:   &avgPrice30d,
		}

		if err := m.db.SaveTradeVolumeCache(bgCtx, newCache); err != nil {
			m.logger.Warn("Failed to save trade volume cache",
				zap.Int("item_id", itemID),
				zap.Error(err))
		} else {
			// Notify via SSE that trade volume was updated for this item
			if m.dataChangeNotifier != nil {
				m.dataChangeNotifier("market:item:" + strconv.Itoa(itemID))
			}
		}
	}()

	// If we have stale cache data, use it for this response
	// The fresh data will be available on next request
	if cache != nil {
		summary.TradeVolume1d = cache.TradeVolume1d
		summary.AvgPrice1d = cache.AvgPrice1d
		summary.AvgPrice7d = cache.AvgPrice7d
		summary.AvgPrice30d = cache.AvgPrice30d
	}
}

// handleGetItemByName returns a single item by name_id
func (m *MarketAPI) handleGetItemByName(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nameID := r.PathValue("nameId")
	if nameID == "" {
		http.Error(w, "Missing name ID", http.StatusBadRequest)
		return
	}

	item, err := m.db.GetItemByNameID(ctx, nameID)
	if err != nil {
		m.logger.Error("Failed to get item by name", zap.Error(err), zap.String("name_id", nameID))
		http.Error(w, "Failed to get item", http.StatusInternalServerError)
		return
	}

	if item == nil {
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	summary, err := m.analytics.GetItemSummary(ctx, item.ID)
	if err != nil {
		m.logger.Error("Failed to get item summary", zap.Error(err), zap.Int("item_id", item.ID))
		http.Error(w, "Failed to get item", http.StatusInternalServerError)
		return
	}

	// Check if we need to refresh trade volume data
	m.refreshTradeVolumeIfNeeded(ctx, item.ID, summary)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// PriceHistoryResponse represents the price history response
type PriceHistoryResponse struct {
	Item    *market.Item            `json:"item"`
	History []market.PriceSnapshot  `json:"history"`
	OHLC    []market.OHLC           `json:"ohlc,omitempty"`
}

// handleGetPriceHistory returns price history for an item
func (m *MarketAPI) handleGetPriceHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	itemIDStr := r.PathValue("itemId")
	itemID, err := strconv.Atoi(itemIDStr)
	if err != nil {
		http.Error(w, "Invalid item ID", http.StatusBadRequest)
		return
	}

	// Parse time range
	from := time.Now().UTC().Add(-24 * time.Hour)
	to := time.Now().UTC()

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = parsed
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = parsed
		}
	}

	// Parse limit
	limit := 1000
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 10000 {
			limit = parsed
		}
	}

	// Parse interval for OHLC
	interval := 60 // default 1 hour
	if i := r.URL.Query().Get("interval"); i != "" {
		if parsed, err := strconv.Atoi(i); err == nil && parsed > 0 {
			interval = parsed
		}
	}

	item, err := m.db.GetItem(ctx, itemID)
	if err != nil || item == nil {
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	// Use bucketed query if interval is specified and small (for 1h view use 1 minute buckets)
	var history []market.PriceSnapshot
	if interval <= 5 {
		// For short intervals (1-5 min), bucket the data
		history, err = m.db.GetPriceHistoryBucketed(ctx, itemID, from, to, interval, limit)
	} else {
		// For longer intervals, use raw data
		history, err = m.db.GetPriceHistory(ctx, itemID, from, to, limit)
	}
	if err != nil {
		m.logger.Error("Failed to get price history", zap.Error(err), zap.Int("item_id", itemID))
		http.Error(w, "Failed to get price history", http.StatusInternalServerError)
		return
	}

	response := PriceHistoryResponse{
		Item:    item,
		History: history,
	}

	// Include OHLC if requested
	if r.URL.Query().Get("ohlc") == "true" {
		ohlc, err := m.analytics.GetOHLC(ctx, itemID, from, to, interval)
		if err == nil {
			response.OHLC = ohlc
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetDailyPrices returns daily aggregated prices for an item
func (m *MarketAPI) handleGetDailyPrices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	itemIDStr := r.PathValue("itemId")
	itemID, err := strconv.Atoi(itemIDStr)
	if err != nil {
		http.Error(w, "Invalid item ID", http.StatusBadRequest)
		return
	}

	// Parse time range (default 30 days)
	from := time.Now().UTC().Add(-30 * 24 * time.Hour)
	to := time.Now().UTC()

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if parsed, err := time.Parse("2006-01-02", fromStr); err == nil {
			from = parsed
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if parsed, err := time.Parse("2006-01-02", toStr); err == nil {
			to = parsed
		}
	}

	item, err := m.db.GetItem(ctx, itemID)
	if err != nil || item == nil {
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	aggregates, err := m.db.GetDailyAggregates(ctx, itemID, from, to)
	if err != nil {
		m.logger.Error("Failed to get daily aggregates", zap.Error(err), zap.Int("item_id", itemID))
		http.Error(w, "Failed to get daily prices", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"item":   item,
		"daily":  aggregates,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetOHLC returns OHLC candlestick data for charting
func (m *MarketAPI) handleGetOHLC(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	itemIDStr := r.PathValue("itemId")
	itemID, err := strconv.Atoi(itemIDStr)
	if err != nil {
		http.Error(w, "Invalid item ID", http.StatusBadRequest)
		return
	}

	// Parse time range
	from := time.Now().UTC().Add(-7 * 24 * time.Hour)
	to := time.Now().UTC()

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = parsed
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = parsed
		}
	}

	// Parse interval (default 60 minutes)
	interval := 60
	if i := r.URL.Query().Get("interval"); i != "" {
		if parsed, err := strconv.Atoi(i); err == nil && parsed > 0 {
			interval = parsed
		}
	}

	ohlc, err := m.analytics.GetOHLC(ctx, itemID, from, to, interval)
	if err != nil {
		m.logger.Error("Failed to get OHLC", zap.Error(err), zap.Int("item_id", itemID))
		http.Error(w, "Failed to get OHLC data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ohlc)
}

// handleGetTopMovers returns top gaining or losing items
func (m *MarketAPI) handleGetTopMovers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse parameters
	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 && parsed <= 168 {
			hours = parsed
		}
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	gainers := r.URL.Query().Get("type") != "losers"

	movers, err := m.db.GetTopMovers(ctx, hours, limit, gainers)
	if err != nil {
		m.logger.Error("Failed to get top movers", zap.Error(err))
		http.Error(w, "Failed to get top movers", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(movers)
}

// handleGetMostTraded returns most traded items
func (m *MarketAPI) handleGetMostTraded(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 && parsed <= 168 {
			hours = parsed
		}
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	traded, err := m.db.GetMostTraded(ctx, hours, limit)
	if err != nil {
		m.logger.Error("Failed to get most traded", zap.Error(err))
		http.Error(w, "Failed to get most traded", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(traded)
}

// handleGetSpreadAnalysis returns spread analysis for an item
func (m *MarketAPI) handleGetSpreadAnalysis(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	itemIDStr := r.PathValue("itemId")
	itemID, err := strconv.Atoi(itemIDStr)
	if err != nil {
		http.Error(w, "Invalid item ID", http.StatusBadRequest)
		return
	}

	analysis, err := m.analytics.GetSpreadAnalysis(ctx, itemID)
	if err != nil {
		m.logger.Error("Failed to get spread analysis", zap.Error(err), zap.Int("item_id", itemID))
		http.Error(w, "Failed to get spread analysis", http.StatusInternalServerError)
		return
	}

	if analysis == nil {
		http.Error(w, "No data available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(analysis)
}

// handleGetVolumeAnalysis returns volume analysis for an item
func (m *MarketAPI) handleGetVolumeAnalysis(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	itemIDStr := r.PathValue("itemId")
	itemID, err := strconv.Atoi(itemIDStr)
	if err != nil {
		http.Error(w, "Invalid item ID", http.StatusBadRequest)
		return
	}

	analysis, err := m.analytics.GetVolumeAnalysis(ctx, itemID)
	if err != nil {
		m.logger.Error("Failed to get volume analysis", zap.Error(err), zap.Int("item_id", itemID))
		http.Error(w, "Failed to get volume analysis", http.StatusInternalServerError)
		return
	}

	if analysis == nil {
		http.Error(w, "No data available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(analysis)
}

// handleGetMovingAverage returns moving average data for an item
func (m *MarketAPI) handleGetMovingAverage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	itemIDStr := r.PathValue("itemId")
	itemID, err := strconv.Atoi(itemIDStr)
	if err != nil {
		http.Error(w, "Invalid item ID", http.StatusBadRequest)
		return
	}

	// Parse time range
	from := time.Now().UTC().Add(-7 * 24 * time.Hour)
	to := time.Now().UTC()

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = parsed
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = parsed
		}
	}

	// Parse window (default 60 minutes)
	window := 60
	if w := r.URL.Query().Get("window"); w != "" {
		if parsed, err := strconv.Atoi(w); err == nil && parsed > 0 {
			window = parsed
		}
	}

	ma, err := m.analytics.GetMovingAverage(ctx, itemID, from, to, window)
	if err != nil {
		m.logger.Error("Failed to get moving average", zap.Error(err), zap.Int("item_id", itemID))
		http.Error(w, "Failed to get moving average", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ma)
}

// handleBackfillStatus returns the backfill status
func (m *MarketAPI) handleBackfillStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	needsBackfill := m.collector.NeedsBackfill(ctx)
	stats, _ := m.collector.GetCollectionStats(ctx)

	response := map[string]interface{}{
		"needs_backfill": needsBackfill,
		"stats":          stats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleTriggerBackfill starts a historical data backfill
func (m *MarketAPI) handleTriggerBackfill(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	forceParam := r.URL.Query().Get("force")

	// Check if backfill is already done
	if !m.collector.NeedsBackfill(ctx) {
		if forceParam != "true" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "skipped",
				"message": "Backfill already completed. Use ?force=true to re-run.",
			})
			return
		}

		// Force mode: reset all backfill flags so items get re-processed
		m.logger.Info("Force backfill requested, resetting history_backfilled flags")
		if err := m.db.ResetBackfillFlags(ctx); err != nil {
			m.logger.Error("Failed to reset backfill flags", zap.Error(err))
			http.Error(w, "Failed to reset backfill flags", http.StatusInternalServerError)
			return
		}
	}

	// Start backfill in a goroutine with background context
	// (request context would be cancelled when the HTTP response is sent)
	go func() {
		bgCtx := context.Background()
		if err := m.collector.BackfillAllHistory(bgCtx); err != nil {
			m.logger.Error("Backfill failed", zap.Error(err))
		}
		// Refresh continuous aggregates after backfill
		if err := m.db.RefreshContinuousAggregates(bgCtx); err != nil {
			m.logger.Warn("Failed to refresh continuous aggregates", zap.Error(err))
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "started",
		"message": "Historical data backfill started. Check /api/market/stats for progress.",
	})
}

// CreateWatchRequest represents a request to create a market watch
type CreateWatchRequest struct {
	ItemID    int    `json:"item_id"`
	WatchType string `json:"watch_type"` // "buy" or "sell"
	Threshold int    `json:"threshold"`
}

// handleCreateWatch creates a new market watch for the authenticated user
func (m *MarketAPI) handleCreateWatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req CreateWatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.ItemID <= 0 {
		http.Error(w, "Invalid item ID", http.StatusBadRequest)
		return
	}
	if req.WatchType != "buy" && req.WatchType != "sell" {
		http.Error(w, "Watch type must be 'buy' or 'sell'", http.StatusBadRequest)
		return
	}
	if req.Threshold <= 0 {
		http.Error(w, "Threshold must be positive", http.StatusBadRequest)
		return
	}

	// Check if item exists
	item, err := m.db.GetItem(ctx, req.ItemID)
	if err != nil || item == nil {
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	// Check watch limit (max 10 active watches per user)
	count, err := m.db.GetWatchCountByUser(ctx, session.UserID)
	if err != nil {
		m.logger.Error("Failed to get watch count", zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if count >= 10 {
		http.Error(w, "Maximum of 10 active watches allowed", http.StatusBadRequest)
		return
	}

	// Create the watch
	watch, err := m.db.CreateWatch(ctx, session.UserID, req.ItemID, req.WatchType, req.Threshold)
	if err != nil {
		m.logger.Error("Failed to create watch", zap.Error(err))
		http.Error(w, "Failed to create watch", http.StatusInternalServerError)
		return
	}

	m.logger.Info("Market watch created",
		zap.Int("watch_id", watch.ID),
		zap.String("user_id", session.UserID),
		zap.Int("item_id", req.ItemID),
		zap.String("type", req.WatchType),
		zap.Int("threshold", req.Threshold))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(watch)
}

// handleGetWatches returns all watches for the authenticated user
func (m *MarketAPI) handleGetWatches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	watches, err := m.db.GetWatchesByUser(ctx, session.UserID)
	if err != nil {
		m.logger.Error("Failed to get watches", zap.Error(err), zap.String("user_id", session.UserID))
		http.Error(w, "Failed to get watches", http.StatusInternalServerError)
		return
	}

	// Ensure we return an empty array, not null
	if watches == nil {
		watches = []market.WatchWithItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(watches)
}

// handleDeleteWatch deletes a watch owned by the authenticated user
func (m *MarketAPI) handleDeleteWatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session := getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	watchIDStr := r.PathValue("watchId")
	watchID, err := strconv.Atoi(watchIDStr)
	if err != nil {
		http.Error(w, "Invalid watch ID", http.StatusBadRequest)
		return
	}

	err = m.db.DeleteWatch(ctx, watchID, session.UserID)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			http.Error(w, "Watch not found", http.StatusNotFound)
			return
		}
		m.logger.Error("Failed to delete watch", zap.Error(err), zap.Int("watch_id", watchID))
		http.Error(w, "Failed to delete watch", http.StatusInternalServerError)
		return
	}

	m.logger.Info("Market watch deleted",
		zap.Int("watch_id", watchID),
		zap.String("user_id", session.UserID))

	w.WriteHeader(http.StatusNoContent)
}

// RegisterMarketRoutes registers all market API routes
func (m *MarketAPI) RegisterRoutes(mux *http.ServeMux, prefix string) {
	// Market overview
	mux.HandleFunc("GET "+prefix+"/overview", m.handleMarketOverview)
	mux.HandleFunc("GET "+prefix+"/stats", m.handleMarketStats)

	// Backfill endpoints
	mux.HandleFunc("GET "+prefix+"/backfill", m.handleBackfillStatus)
	mux.HandleFunc("POST "+prefix+"/backfill", m.handleTriggerBackfill)

	// Item list endpoints
	mux.HandleFunc("GET "+prefix+"/items", m.handleGetAllItems)
	mux.HandleFunc("GET "+prefix+"/items/search", m.handleSearchItems)

	// Item by name (separate path to avoid conflict with {itemId} routes)
	mux.HandleFunc("GET "+prefix+"/item-by-name/{nameId}", m.handleGetItemByName)

	// Item by ID and sub-resources
	mux.HandleFunc("GET "+prefix+"/items/{itemId}", m.handleGetItem)
	mux.HandleFunc("GET "+prefix+"/items/{itemId}/history", m.handleGetPriceHistory)
	mux.HandleFunc("GET "+prefix+"/items/{itemId}/daily", m.handleGetDailyPrices)
	mux.HandleFunc("GET "+prefix+"/items/{itemId}/ohlc", m.handleGetOHLC)
	mux.HandleFunc("GET "+prefix+"/items/{itemId}/spread", m.handleGetSpreadAnalysis)
	mux.HandleFunc("GET "+prefix+"/items/{itemId}/volume", m.handleGetVolumeAnalysis)
	mux.HandleFunc("GET "+prefix+"/items/{itemId}/ma", m.handleGetMovingAverage)
	mux.HandleFunc("GET "+prefix+"/items/{itemId}/og-image.png", m.handleMarketItemOGImage)

	// Top movers
	mux.HandleFunc("GET "+prefix+"/movers", m.handleGetTopMovers)
	mux.HandleFunc("GET "+prefix+"/most-traded", m.handleGetMostTraded)
}

// RegisterAuthenticatedRoutes registers market routes that require authentication
// This should be called by the Server to wrap handlers with withAuth
func (m *MarketAPI) RegisterAuthenticatedRoutes(mux *http.ServeMux, prefix string, withAuth func(http.HandlerFunc) http.HandlerFunc) {
	// Watch endpoints (require authentication)
	mux.HandleFunc("POST "+prefix+"/watches", withAuth(m.handleCreateWatch))
	mux.HandleFunc("GET "+prefix+"/watches", withAuth(m.handleGetWatches))
	mux.HandleFunc("DELETE "+prefix+"/watches/{watchId}", withAuth(m.handleDeleteWatch))
}

