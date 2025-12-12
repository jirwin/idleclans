package web

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jirwin/idleclans/pkg/market"
	"go.uber.org/zap"
)

// Colors for the OG image
var (
	bgColor     = color.RGBA{10, 15, 26, 255}    // #0a0f1a
	textColor   = color.RGBA{243, 244, 246, 255} // #f3f4f6
	greenColor  = color.RGBA{16, 185, 129, 255}  // #10b981
	redColor    = color.RGBA{239, 68, 68, 255}   // #ef4444
	borderColor = color.RGBA{31, 41, 55, 255}    // #1f2937
)

// handleMarketItemOGImage generates an Open Graph PNG image for a market item
func (m *MarketAPI) handleMarketItemOGImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	itemIDStr := r.PathValue("itemId")
	itemID, err := strconv.Atoi(itemIDStr)
	if err != nil {
		http.Error(w, "Invalid item ID", http.StatusBadRequest)
		return
	}

	// Get item info
	item, err := m.db.GetItem(ctx, itemID)
	if err != nil || item == nil {
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	// Get current price
	currentPrice, err := m.db.GetLatestPrice(ctx, itemID)
	if err != nil {
		m.logger.Warn("Failed to get latest price for OG image", zap.Error(err), zap.Int("item_id", itemID))
	}

	// Get price history for chart (last 7 days)
	from := time.Now().UTC().Add(-7 * 24 * time.Hour)
	to := time.Now().UTC()
	history, err := m.db.GetPriceHistory(ctx, itemID, from, to, 200)
	if err != nil {
		m.logger.Warn("Failed to get price history for OG image", zap.Error(err), zap.Int("item_id", itemID))
		history = nil
	}

	// Generate PNG image
	img := generateMarketItemPNG(item.DisplayName, currentPrice, history)

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300") // Cache for 5 minutes
	
	if err := png.Encode(w, img); err != nil {
		m.logger.Error("Failed to encode PNG", zap.Error(err))
		http.Error(w, "Failed to generate image", http.StatusInternalServerError)
	}
}

// generateMarketItemPNG creates a PNG image for a market item
func generateMarketItemPNG(displayName string, currentPrice *market.PriceSnapshot, history []market.PriceSnapshot) image.Image {
	const width = 800
	const height = 320
	const chartMarginLeft = 50
	const chartMarginRight = 50
	const chartMarginTop = 80
	const chartMarginBottom = 50
	const chartWidth = width - chartMarginLeft - chartMarginRight
	const chartHeight = height - chartMarginTop - chartMarginBottom

	// Create image
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill background
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, bgColor)
		}
	}

	// Draw border
	drawRect(img, 0, 0, width-1, height-1, borderColor)
	drawRect(img, 1, 1, width-2, height-2, borderColor)

	// Draw chart area background (slightly lighter)
	chartBg := color.RGBA{13, 19, 33, 255} // #0d1321
	fillRect(img, chartMarginLeft, chartMarginTop, chartMarginLeft+chartWidth, chartMarginTop+chartHeight, chartBg)

	// Draw chart grid lines
	gridColor := color.RGBA{31, 41, 55, 128} // #1f2937 with alpha
	for i := 0; i <= 4; i++ {
		y := chartMarginTop + (chartHeight * i / 4)
		drawHLine(img, chartMarginLeft, chartMarginLeft+chartWidth, y, gridColor)
	}

	// Draw chart if we have history
	if len(history) >= 2 {
		// Find min/max prices
		minPrice := math.MaxInt
		maxPrice := 0
		for _, h := range history {
			if h.LowestSellPrice > 0 {
				if h.LowestSellPrice < minPrice {
					minPrice = h.LowestSellPrice
				}
				if h.LowestSellPrice > maxPrice {
					maxPrice = h.LowestSellPrice
				}
			}
		}

		if minPrice == math.MaxInt {
			minPrice = 0
		}
		
		// Add 10% padding to price range
		priceRange := float64(maxPrice - minPrice)
		if priceRange == 0 {
			priceRange = float64(maxPrice) * 0.1
			if priceRange == 0 {
				priceRange = 1
			}
		}

		// Determine trend color
		firstPrice := 0
		lastPrice := 0
		for _, h := range history {
			if h.LowestSellPrice > 0 {
				if firstPrice == 0 {
					firstPrice = h.LowestSellPrice
				}
				lastPrice = h.LowestSellPrice
			}
		}
		isUp := lastPrice >= firstPrice
		lineColor := greenColor
		if !isUp {
			lineColor = redColor
		}

		// Build points for the chart line
		var points []point
		for i, h := range history {
			if h.LowestSellPrice > 0 {
				x := chartMarginLeft + int(float64(i)/float64(len(history)-1)*float64(chartWidth))
				y := chartMarginTop + int((1-float64(h.LowestSellPrice-minPrice)/priceRange)*float64(chartHeight))
				// Clamp y to chart area
				if y < chartMarginTop {
					y = chartMarginTop
				}
				if y > chartMarginTop+chartHeight {
					y = chartMarginTop + chartHeight
				}
				points = append(points, point{x, y})
			}
		}

		if len(points) >= 2 {
			// Draw filled area under the line
			bottomY := chartMarginTop + chartHeight
			for i := 0; i < len(points)-1; i++ {
				// Fill vertical strips
				x1 := points[i].x
				x2 := points[i+1].x
				y1 := points[i].y
				y2 := points[i+1].y
				
				for x := x1; x <= x2; x++ {
					// Interpolate y
					t := float64(x-x1) / float64(x2-x1+1)
					topY := int(float64(y1) + t*float64(y2-y1))
					for y := topY; y <= bottomY; y++ {
						// Gradient fill - more opaque at top
						alpha := uint8(50 - 40*float64(y-topY)/float64(bottomY-topY))
						c := color.RGBA{lineColor.R, lineColor.G, lineColor.B, alpha}
						blendPixel(img, x, y, c)
					}
				}
			}

			// Draw the line (thicker)
			for i := 0; i < len(points)-1; i++ {
				drawThickLine(img, points[i].x, points[i].y, points[i+1].x, points[i+1].y, lineColor, 3)
			}
		}

		// Draw price indicator dot at the end
		if len(points) > 0 {
			lastPoint := points[len(points)-1]
			drawFilledCircle(img, lastPoint.x, lastPoint.y, 6, lineColor)
			drawFilledCircle(img, lastPoint.x, lastPoint.y, 3, textColor)
		}
	}

	// Draw axis lines
	drawHLine(img, chartMarginLeft, chartMarginLeft+chartWidth, chartMarginTop+chartHeight, borderColor)
	drawVLine(img, chartMarginLeft, chartMarginTop, chartMarginTop+chartHeight, borderColor)

	return img
}

type point struct {
	x, y int
}

// Drawing helper functions

func fillRect(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			img.Set(x, y, c)
		}
	}
}

func drawRect(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	drawHLine(img, x1, x2, y1, c)
	drawHLine(img, x1, x2, y2, c)
	drawVLine(img, x1, y1, y2, c)
	drawVLine(img, x2, y1, y2, c)
}

func drawHLine(img *image.RGBA, x1, x2, y int, c color.Color) {
	for x := x1; x <= x2; x++ {
		img.Set(x, y, c)
	}
}

func drawVLine(img *image.RGBA, x, y1, y2 int, c color.Color) {
	for y := y1; y <= y2; y++ {
		img.Set(x, y, c)
	}
}

func drawThickLine(img *image.RGBA, x1, y1, x2, y2 int, c color.Color, thickness int) {
	// Bresenham's line algorithm with thickness
	dx := abs(x2 - x1)
	dy := abs(y2 - y1)
	sx := 1
	if x1 >= x2 {
		sx = -1
	}
	sy := 1
	if y1 >= y2 {
		sy = -1
	}
	err := dx - dy

	for {
		// Draw thick point
		for ty := -thickness / 2; ty <= thickness/2; ty++ {
			for tx := -thickness / 2; tx <= thickness/2; tx++ {
				img.Set(x1+tx, y1+ty, c)
			}
		}

		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x1 += sx
		}
		if e2 < dx {
			err += dx
			y1 += sy
		}
	}
}

func drawFilledCircle(img *image.RGBA, cx, cy, r int, c color.Color) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				img.Set(cx+x, cy+y, c)
			}
		}
	}
}

func blendPixel(img *image.RGBA, x, y int, c color.RGBA) {
	if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		return
	}
	existing := img.RGBAAt(x, y)
	alpha := float64(c.A) / 255.0
	newR := uint8(float64(existing.R)*(1-alpha) + float64(c.R)*alpha)
	newG := uint8(float64(existing.G)*(1-alpha) + float64(c.G)*alpha)
	newB := uint8(float64(existing.B)*(1-alpha) + float64(c.B)*alpha)
	img.Set(x, y, color.RGBA{newR, newG, newB, 255})
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
