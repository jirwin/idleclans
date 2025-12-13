package market

import (
	"context"
	"math"
	"sort"
	"time"
)

// Analytics provides price analytics and derived metrics
type Analytics struct {
	db *DB
}

// NewAnalytics creates a new analytics instance
func NewAnalytics(db *DB) *Analytics {
	return &Analytics{db: db}
}

// ItemSummary provides a summary of an item's market data
type ItemSummary struct {
	Item           *Item             `json:"item"`
	CurrentPrice   *PriceSnapshot    `json:"current_price"`
	Change24h      *PriceChange24h   `json:"change_24h"`
	Volatility     float64           `json:"volatility"`
	Spread         int               `json:"spread"`
	SpreadPercent  float64           `json:"spread_percent"`
	TradeVolume1d  *int              `json:"trade_volume_1d,omitempty"`
	AvgPrice1d     *int              `json:"avg_price_1d,omitempty"`
	AvgPrice7d     *int              `json:"avg_price_7d,omitempty"`
	AvgPrice30d    *int              `json:"avg_price_30d,omitempty"`
}

// PriceChange24h represents 24-hour price change data
type PriceChange24h struct {
	PreviousPrice int     `json:"previous_price"`
	CurrentPrice  int     `json:"current_price"`
	Change        int     `json:"change"`
	ChangePercent float64 `json:"change_percent"`
}

// GetItemSummary retrieves a comprehensive summary for an item
func (a *Analytics) GetItemSummary(ctx context.Context, itemID int) (*ItemSummary, error) {
	item, err := a.db.GetItem(ctx, itemID)
	if err != nil || item == nil {
		return nil, err
	}

	current, err := a.db.GetLatestPrice(ctx, itemID)
	if err != nil {
		return nil, err
	}

	summary := &ItemSummary{
		Item:         item,
		CurrentPrice: current,
	}

	if current != nil {
		// Calculate spread
		summary.Spread = current.LowestSellPrice - current.HighestBuyPrice
		if current.LowestSellPrice > 0 {
			summary.SpreadPercent = float64(summary.Spread) / float64(current.LowestSellPrice) * 100
		}

		// Calculate 24h change
		summary.Change24h = a.calculate24hChange(ctx, itemID, current.LowestSellPrice)

		// Calculate volatility
		summary.Volatility = a.calculateVolatility(ctx, itemID, 24*time.Hour)
	}

	// Get trade volume from cache if available
	tradeVol, err := a.db.GetTradeVolumeCache(ctx, itemID)
	if err == nil && tradeVol != nil {
		summary.TradeVolume1d = tradeVol.TradeVolume1d
		summary.AvgPrice1d = tradeVol.AvgPrice1d
		summary.AvgPrice7d = tradeVol.AvgPrice7d
		summary.AvgPrice30d = tradeVol.AvgPrice30d
	}

	return summary, nil
}

func (a *Analytics) calculate24hChange(ctx context.Context, itemID int, currentPrice int) *PriceChange24h {
	from := time.Now().UTC().Add(-25 * time.Hour)
	to := time.Now().UTC().Add(-23 * time.Hour)

	history, err := a.db.GetPriceHistory(ctx, itemID, from, to, 1)
	if err != nil || len(history) == 0 {
		return nil
	}

	prevPrice := history[0].LowestSellPrice
	if prevPrice == 0 {
		return nil
	}

	change := currentPrice - prevPrice
	changePercent := float64(change) / float64(prevPrice) * 100

	return &PriceChange24h{
		PreviousPrice: prevPrice,
		CurrentPrice:  currentPrice,
		Change:        change,
		ChangePercent: changePercent,
	}
}

func (a *Analytics) calculateVolatility(ctx context.Context, itemID int, duration time.Duration) float64 {
	from := time.Now().UTC().Add(-duration)
	to := time.Now().UTC()

	history, err := a.db.GetPriceHistory(ctx, itemID, from, to, 1000)
	if err != nil || len(history) < 2 {
		return 0
	}

	// Calculate returns
	returns := make([]float64, 0, len(history)-1)
	for i := 1; i < len(history); i++ {
		if history[i-1].LowestSellPrice > 0 && history[i].LowestSellPrice > 0 {
			ret := float64(history[i].LowestSellPrice-history[i-1].LowestSellPrice) / float64(history[i-1].LowestSellPrice)
			returns = append(returns, ret)
		}
	}

	if len(returns) < 2 {
		return 0
	}

	// Calculate standard deviation of returns
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		variance += (r - mean) * (r - mean)
	}
	variance /= float64(len(returns))

	return math.Sqrt(variance) * 100 // Return as percentage
}

// OHLC represents Open-High-Low-Close candlestick data
type OHLC struct {
	Time   time.Time `json:"time"`
	Open   int       `json:"open"`
	High   int       `json:"high"`
	Low    int       `json:"low"`
	Close  int       `json:"close"`
	Volume int64     `json:"volume"`
}

// GetOHLC retrieves OHLC candlestick data for charting
func (a *Analytics) GetOHLC(ctx context.Context, itemID int, from, to time.Time, intervalMinutes int) ([]OHLC, error) {
	history, err := a.db.GetPriceHistory(ctx, itemID, from, to, 10000)
	if err != nil {
		return nil, err
	}

	if len(history) == 0 {
		return []OHLC{}, nil
	}

	// Sort by time ascending
	sort.Slice(history, func(i, j int) bool {
		return history[i].Time.Before(history[j].Time)
	})

	interval := time.Duration(intervalMinutes) * time.Minute
	ohlcMap := make(map[time.Time]*OHLC)

	for _, h := range history {
		bucket := h.Time.Truncate(interval)
		
		candle, exists := ohlcMap[bucket]
		if !exists {
			candle = &OHLC{
				Time:   bucket,
				Open:   h.LowestSellPrice,
				High:   h.LowestSellPrice,
				Low:    h.LowestSellPrice,
				Close:  h.LowestSellPrice,
				Volume: int64(h.LowestPriceVolume),
			}
			ohlcMap[bucket] = candle
		} else {
			if h.LowestSellPrice > candle.High {
				candle.High = h.LowestSellPrice
			}
			if h.LowestSellPrice < candle.Low && h.LowestSellPrice > 0 {
				candle.Low = h.LowestSellPrice
			}
			candle.Close = h.LowestSellPrice
			candle.Volume += int64(h.LowestPriceVolume)
		}
	}

	// Convert map to sorted slice
	result := make([]OHLC, 0, len(ohlcMap))
	for _, candle := range ohlcMap {
		result = append(result, *candle)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Time.Before(result[j].Time)
	})

	return result, nil
}

// MovingAverage represents a moving average data point
type MovingAverage struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}

// GetMovingAverage calculates simple moving average
func (a *Analytics) GetMovingAverage(ctx context.Context, itemID int, from, to time.Time, windowMinutes int) ([]MovingAverage, error) {
	history, err := a.db.GetPriceHistory(ctx, itemID, from, to, 10000)
	if err != nil {
		return nil, err
	}

	if len(history) == 0 {
		return []MovingAverage{}, nil
	}

	// Sort by time ascending
	sort.Slice(history, func(i, j int) bool {
		return history[i].Time.Before(history[j].Time)
	})

	window := time.Duration(windowMinutes) * time.Minute
	result := make([]MovingAverage, 0)

	for i, h := range history {
		// Find all prices within the window
		windowStart := h.Time.Add(-window)
		sum := 0.0
		count := 0

		for j := i; j >= 0 && history[j].Time.After(windowStart); j-- {
			if history[j].LowestSellPrice > 0 {
				sum += float64(history[j].LowestSellPrice)
				count++
			}
		}

		if count > 0 {
			result = append(result, MovingAverage{
				Time:  h.Time,
				Value: sum / float64(count),
			})
		}
	}

	return result, nil
}

// MarketOverview provides an overview of the market
type MarketOverview struct {
	TotalItems      int           `json:"total_items"`
	ActiveItems     int           `json:"active_items"`
	TopGainers      []PriceChange `json:"top_gainers"`
	TopLosers       []PriceChange `json:"top_losers"`
	MostTraded      []PriceChange `json:"most_traded"`
	LastUpdated     time.Time     `json:"last_updated"`
}

// GetMarketOverview retrieves a market overview
// Uses cached data if available (updated by collector), falls back to live queries
func (a *Analytics) GetMarketOverview(ctx context.Context) (*MarketOverview, error) {
	// Try to get cached overview first (fast path)
	cached, err := a.db.GetCachedMarketOverview(ctx)
	if err == nil && cached != nil && time.Since(cached.UpdatedAt) < 5*time.Minute {
		// Cache is fresh, use it
		return &MarketOverview{
			TotalItems:  cached.TotalItems,
			ActiveItems: cached.ActiveItems,
			TopGainers:  cached.TopGainers,
			TopLosers:   cached.TopLosers,
			MostTraded:  cached.MostTraded,
			LastUpdated: cached.UpdatedAt,
		}, nil
	}

	// Cache miss or stale - compute live (slower fallback)
	totalItems, err := a.db.GetItemCount(ctx)
	if err != nil {
		return nil, err
	}

	topGainers, err := a.db.GetTopMovers(ctx, 24, 10, true)
	if err != nil {
		return nil, err
	}

	topLosers, err := a.db.GetTopMovers(ctx, 24, 10, false)
	if err != nil {
		return nil, err
	}

	mostTraded, err := a.db.GetMostTraded(ctx, 24, 10)
	if err != nil {
		return nil, err
	}

	// Count active items (items with price data in last hour)
	activeItems, err := a.db.GetActiveItemCount(ctx)
	if err != nil {
		activeItems = 0 // fallback to 0 on error
	}

	return &MarketOverview{
		TotalItems:  totalItems,
		ActiveItems: activeItems,
		TopGainers:  topGainers,
		TopLosers:   topLosers,
		MostTraded:  mostTraded,
		LastUpdated: time.Now().UTC(),
	}, nil
}

// RefreshMarketOverviewCache pre-computes and caches the market overview
// This should be called by the collector after each collection cycle
func (a *Analytics) RefreshMarketOverviewCache(ctx context.Context) error {
	totalItems, err := a.db.GetItemCount(ctx)
	if err != nil {
		return err
	}

	activeItems, err := a.db.GetActiveItemCount(ctx)
	if err != nil {
		activeItems = 0
	}

	// Use optimized queries for cache refresh
	gainers, err := a.db.GetTopMoversOptimized(ctx, 24, 10, true)
	if err != nil {
		return err
	}

	losers, err := a.db.GetTopMoversOptimized(ctx, 24, 10, false)
	if err != nil {
		return err
	}

	mostTraded, err := a.db.GetMostTradedOptimized(ctx, 10)
	if err != nil {
		return err
	}

	return a.db.UpdateCachedMarketOverview(ctx, totalItems, activeItems, gainers, losers, mostTraded)
}

// PriceAlert represents a price alert condition
type PriceAlert struct {
	ItemID      int       `json:"item_id"`
	Type        string    `json:"type"` // "above", "below", "change_percent"
	Threshold   float64   `json:"threshold"`
	Triggered   bool      `json:"triggered"`
	TriggeredAt time.Time `json:"triggered_at,omitempty"`
}

// CheckPriceAlert checks if a price alert condition is met
func (a *Analytics) CheckPriceAlert(ctx context.Context, alert *PriceAlert) (bool, error) {
	current, err := a.db.GetLatestPrice(ctx, alert.ItemID)
	if err != nil || current == nil {
		return false, err
	}

	switch alert.Type {
	case "above":
		return float64(current.LowestSellPrice) > alert.Threshold, nil
	case "below":
		return float64(current.LowestSellPrice) < alert.Threshold, nil
	case "change_percent":
		change := a.calculate24hChange(ctx, alert.ItemID, current.LowestSellPrice)
		if change == nil {
			return false, nil
		}
		return math.Abs(change.ChangePercent) > alert.Threshold, nil
	default:
		return false, nil
	}
}

// SpreadAnalysis provides spread analysis for an item
type SpreadAnalysis struct {
	CurrentSpread    int     `json:"current_spread"`
	CurrentSpreadPct float64 `json:"current_spread_pct"`
	AvgSpread24h     float64 `json:"avg_spread_24h"`
	MinSpread24h     int     `json:"min_spread_24h"`
	MaxSpread24h     int     `json:"max_spread_24h"`
	SpreadVolatility float64 `json:"spread_volatility"`
}

// GetSpreadAnalysis analyzes bid-ask spread for an item
func (a *Analytics) GetSpreadAnalysis(ctx context.Context, itemID int) (*SpreadAnalysis, error) {
	from := time.Now().UTC().Add(-24 * time.Hour)
	to := time.Now().UTC()

	history, err := a.db.GetPriceHistory(ctx, itemID, from, to, 1000)
	if err != nil {
		return nil, err
	}

	if len(history) == 0 {
		return nil, nil
	}

	current := history[0]
	currentSpread := current.LowestSellPrice - current.HighestBuyPrice
	currentSpreadPct := 0.0
	if current.LowestSellPrice > 0 {
		currentSpreadPct = float64(currentSpread) / float64(current.LowestSellPrice) * 100
	}

	// Calculate spread statistics
	spreads := make([]float64, 0, len(history))
	minSpread := currentSpread
	maxSpread := currentSpread
	sumSpread := 0.0

	for _, h := range history {
		spread := h.LowestSellPrice - h.HighestBuyPrice
		if spread >= 0 {
			spreads = append(spreads, float64(spread))
			sumSpread += float64(spread)
			if spread < minSpread {
				minSpread = spread
			}
			if spread > maxSpread {
				maxSpread = spread
			}
		}
	}

	avgSpread := 0.0
	spreadVolatility := 0.0
	if len(spreads) > 0 {
		avgSpread = sumSpread / float64(len(spreads))

		// Calculate spread volatility (standard deviation)
		variance := 0.0
		for _, s := range spreads {
			variance += (s - avgSpread) * (s - avgSpread)
		}
		variance /= float64(len(spreads))
		spreadVolatility = math.Sqrt(variance)
	}

	return &SpreadAnalysis{
		CurrentSpread:    currentSpread,
		CurrentSpreadPct: currentSpreadPct,
		AvgSpread24h:     avgSpread,
		MinSpread24h:     minSpread,
		MaxSpread24h:     maxSpread,
		SpreadVolatility: spreadVolatility,
	}, nil
}

// VolumeAnalysis provides volume analysis for an item
type VolumeAnalysis struct {
	CurrentSellVolume int     `json:"current_sell_volume"`
	CurrentBuyVolume  int     `json:"current_buy_volume"`
	AvgVolume24h      float64 `json:"avg_volume_24h"`
	VolumeChange      float64 `json:"volume_change"` // vs previous 24h
	IsHighVolume      bool    `json:"is_high_volume"` // > 2x average
}

// GetVolumeAnalysis analyzes trading volume for an item
func (a *Analytics) GetVolumeAnalysis(ctx context.Context, itemID int) (*VolumeAnalysis, error) {
	current, err := a.db.GetLatestPrice(ctx, itemID)
	if err != nil || current == nil {
		return nil, err
	}

	// Get last 24h data
	from24h := time.Now().UTC().Add(-24 * time.Hour)
	to := time.Now().UTC()
	history24h, err := a.db.GetPriceHistory(ctx, itemID, from24h, to, 1000)
	if err != nil {
		return nil, err
	}

	// Get previous 24h data (24-48h ago)
	from48h := time.Now().UTC().Add(-48 * time.Hour)
	to24h := time.Now().UTC().Add(-24 * time.Hour)
	historyPrev, err := a.db.GetPriceHistory(ctx, itemID, from48h, to24h, 1000)
	if err != nil {
		return nil, err
	}

	// Calculate average volumes
	var sumVol24h, sumVolPrev int64
	for _, h := range history24h {
		sumVol24h += int64(h.LowestPriceVolume)
	}
	for _, h := range historyPrev {
		sumVolPrev += int64(h.LowestPriceVolume)
	}

	avgVol24h := 0.0
	if len(history24h) > 0 {
		avgVol24h = float64(sumVol24h) / float64(len(history24h))
	}

	volChange := 0.0
	if sumVolPrev > 0 {
		volChange = float64(sumVol24h-sumVolPrev) / float64(sumVolPrev) * 100
	}

	return &VolumeAnalysis{
		CurrentSellVolume: current.LowestPriceVolume,
		CurrentBuyVolume:  current.HighestPriceVolume,
		AvgVolume24h:      avgVol24h,
		VolumeChange:      volChange,
		IsHighVolume:      float64(current.LowestPriceVolume) > avgVol24h*2,
	}, nil
}

