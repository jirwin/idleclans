import { useEffect, useState, useRef, useCallback, useMemo } from 'react';
import { fetchPriceHistory, fetchOHLC, type OHLC, type PriceSnapshot } from '../api';

interface PriceChartProps {
  itemId: number;
  timeRange: '1h' | '24h' | '7d' | '30d';
}

export function PriceChart({ itemId, timeRange }: PriceChartProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [data, setData] = useState<OHLC[]>([]);
  const [rawData, setRawData] = useState<PriceSnapshot[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [hoveredPoint, setHoveredPoint] = useState<{ x: number; y: number; data: OHLC | PriceSnapshot } | null>(null);
  const [chartMode, setChartMode] = useState<'line' | 'candle'>('line');
  
  // Line visibility toggles
  const [showSell, setShowSell] = useState(true);
  const [showBuy, setShowBuy] = useState(true);
  const [showAvg, setShowAvg] = useState(true);
  const [showVolume, setShowVolume] = useState(true);

  // Calculate time range
  const timeParams = useMemo(() => {
    const now = new Date();
    let from: Date;
    let interval: number;

    switch (timeRange) {
      case '1h':
        from = new Date(now.getTime() - 60 * 60 * 1000);
        interval = 5; // 5 minute candles
        break;
      case '24h':
        from = new Date(now.getTime() - 24 * 60 * 60 * 1000);
        interval = 60; // 1 hour candles
        break;
      case '7d':
        from = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
        interval = 240; // 4 hour candles
        break;
      case '30d':
        from = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
        interval = 1440; // 1 day candles
        break;
    }

    return {
      from: from.toISOString(),
      to: now.toISOString(),
      interval,
    };
  }, [timeRange]);

  // Fetch data
  useEffect(() => {
    let cancelled = false;

    async function loadData() {
      setLoading(true);
      setError(null);

      try {
        const [ohlcResponse, historyResponse] = await Promise.all([
          fetchOHLC(itemId, timeParams),
          fetchPriceHistory(itemId, {
            from: timeParams.from,
            to: timeParams.to,
            limit: 1000,
          }),
        ]);

        if (!cancelled) {
          setData(ohlcResponse ?? []);
          setRawData(historyResponse?.history ?? []);
        }
      } catch (err) {
        if (!cancelled) {
          setError('Failed to load chart data');
          console.error(err);
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    loadData();
    
    // Auto-refresh chart data - faster for shorter time ranges
    const refreshInterval = timeRange === '1h' ? 30 * 1000 : 2 * 60 * 1000; // 30s for 1h, 2min otherwise
    const interval = setInterval(loadData, refreshInterval);
    
    return () => { 
      cancelled = true; 
      clearInterval(interval);
    };
  }, [itemId, timeParams, timeRange]);

  // Draw chart
  const drawChart = useCallback(() => {
    const canvas = canvasRef.current;
    const container = containerRef.current;
    if (!canvas || !container) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    // Set canvas size
    const rect = container.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    canvas.style.width = `${rect.width}px`;
    canvas.style.height = `${rect.height}px`;
    ctx.scale(dpr, dpr);

    const width = rect.width;
    const height = rect.height;
    const padding = { top: 20, right: 60, bottom: 40, left: 10 };
    const chartWidth = width - padding.left - padding.right;
    
    // Split height between price chart and volume (if showing volume)
    const volumeHeight = showVolume ? 60 : 0;
    const chartHeight = height - padding.top - padding.bottom - volumeHeight;

    // Clear canvas
    ctx.fillStyle = '#0d1321';
    ctx.fillRect(0, 0, width, height);

    // Get data to draw
    const chartData = chartMode === 'candle' ? data : rawData;
    if (!chartData || chartData.length === 0) {
      ctx.fillStyle = '#6b7280';
      ctx.font = '14px system-ui';
      ctx.textAlign = 'center';
      ctx.fillText('No data available', width / 2, height / 2);
      return;
    }

    // Calculate time range from the requested timeParams (not from data)
    const timeFrom = new Date(timeParams.from).getTime();
    const timeTo = new Date(timeParams.to).getTime();
    const timeSpan = timeTo - timeFrom;

    // Calculate price range (include sell, buy, and average prices)
    let minPrice = Infinity;
    let maxPrice = -Infinity;

    chartData.forEach(d => {
      const prices = chartMode === 'candle'
        ? [(d as OHLC).low, (d as OHLC).high]
        : [(d as PriceSnapshot).lowest_sell_price, (d as PriceSnapshot).highest_buy_price];
      prices.forEach(p => {
        if (p > 0) {
          minPrice = Math.min(minPrice, p);
          maxPrice = Math.max(maxPrice, p);
        }
      });
    });

    // Add padding to price range
    const priceRange = maxPrice - minPrice;
    minPrice = Math.max(0, minPrice - priceRange * 0.1);
    maxPrice = maxPrice + priceRange * 0.1;

    // Helper functions - scale by actual time, not array index
    const xScaleTime = (timestamp: number) => {
      const position = (timestamp - timeFrom) / timeSpan;
      return padding.left + position * chartWidth;
    };
    const yScale = (price: number) => padding.top + chartHeight - ((price - minPrice) / (maxPrice - minPrice || 1)) * chartHeight;

    // Draw grid lines
    ctx.strokeStyle = '#1f2937';
    ctx.lineWidth = 1;
    const gridLines = 5;
    for (let i = 0; i <= gridLines; i++) {
      const y = padding.top + (i / gridLines) * chartHeight;
      ctx.beginPath();
      ctx.moveTo(padding.left, y);
      ctx.lineTo(width - padding.right, y);
      ctx.stroke();

      // Price labels
      const price = maxPrice - (i / gridLines) * (maxPrice - minPrice);
      ctx.fillStyle = '#6b7280';
      ctx.font = '11px system-ui';
      ctx.textAlign = 'left';
      ctx.fillText(formatPrice(price), width - padding.right + 5, y + 4);
    }

    // Draw data
    if (chartMode === 'line') {
      // Line chart - use time-based x-axis
      const validData = rawData.filter(d => (d.lowest_sell_price > 0 || d.highest_buy_price > 0) && d.time);
      
      // Show message if very limited data
      if (validData.length === 0) {
        ctx.fillStyle = '#6b7280';
        ctx.font = '14px system-ui';
        ctx.textAlign = 'center';
        ctx.fillText('No price data for this period', width / 2, height / 2);
      } else if (validData.length < 3) {
        ctx.fillStyle = '#6b7280';
        ctx.font = '12px system-ui';
        ctx.textAlign = 'center';
        ctx.fillText(`Only ${validData.length} data point${validData.length > 1 ? 's' : ''} available`, width / 2, padding.top + 20);
      }

      if (validData.length > 0) {
        // Helper to draw a price line
        const drawPriceLine = (
          getData: (d: PriceSnapshot) => number,
          color: string,
          fillGradient?: boolean
        ) => {
          const points = validData.filter(d => getData(d) > 0);
          if (points.length === 0) return;

          if (fillGradient) {
            // Draw area fill for average line
            ctx.beginPath();
            const firstX = xScaleTime(new Date(points[0].time).getTime());
            ctx.moveTo(firstX, yScale(getData(points[0])));
            points.forEach(d => {
              const x = xScaleTime(new Date(d.time).getTime());
              ctx.lineTo(x, yScale(getData(d)));
            });
            const lastX = xScaleTime(new Date(points[points.length - 1].time).getTime());
            ctx.lineTo(lastX, padding.top + chartHeight);
            ctx.lineTo(firstX, padding.top + chartHeight);
            ctx.closePath();
            const gradient = ctx.createLinearGradient(0, padding.top, 0, padding.top + chartHeight);
            gradient.addColorStop(0, 'rgba(251, 191, 36, 0.15)');
            gradient.addColorStop(1, 'rgba(251, 191, 36, 0)');
            ctx.fillStyle = gradient;
            ctx.fill();
          }

          // Draw line
          ctx.beginPath();
          const firstX = xScaleTime(new Date(points[0].time).getTime());
          ctx.moveTo(firstX, yScale(getData(points[0])));
          points.forEach(d => {
            const x = xScaleTime(new Date(d.time).getTime());
            ctx.lineTo(x, yScale(getData(d)));
          });
          ctx.strokeStyle = color;
          ctx.lineWidth = 2;
          ctx.stroke();
        };

        // Calculate average price for each point
        const getAverage = (d: PriceSnapshot) => {
          const sell = d.lowest_sell_price || 0;
          const buy = d.highest_buy_price || 0;
          if (sell > 0 && buy > 0) return Math.round((sell + buy) / 2);
          return sell || buy;
        };

        // Draw lines in order: fill first, then lines on top
        // Average (amber) - with fill
        if (showAvg) {
          drawPriceLine(getAverage, '#fbbf24', true);
        }
        
        // Sell price (green)
        if (showSell) {
          drawPriceLine(d => d.lowest_sell_price, '#10b981');
        }
        
        // Buy price (cyan)
        if (showBuy) {
          drawPriceLine(d => d.highest_buy_price, '#06b6d4');
        }

        // Draw dots at each data point for visible lines
        validData.forEach(d => {
          const x = xScaleTime(new Date(d.time).getTime());
          
          if (showAvg) {
            const avg = getAverage(d);
            if (avg > 0) {
              ctx.beginPath();
              ctx.arc(x, yScale(avg), 3, 0, Math.PI * 2);
              ctx.fillStyle = '#fbbf24';
              ctx.fill();
            }
          }
          
          if (showSell && d.lowest_sell_price > 0) {
            ctx.beginPath();
            ctx.arc(x, yScale(d.lowest_sell_price), 2, 0, Math.PI * 2);
            ctx.fillStyle = '#10b981';
            ctx.fill();
          }
          
          if (showBuy && d.highest_buy_price > 0) {
            ctx.beginPath();
            ctx.arc(x, yScale(d.highest_buy_price), 2, 0, Math.PI * 2);
            ctx.fillStyle = '#06b6d4';
            ctx.fill();
          }
        });
      }
    } else {
      // Candlestick chart - use time-based x-axis
      const candleWidth = Math.max(1, Math.min(20, (chartWidth / data.length) * 0.6));

      data.forEach(candle => {
        if (!candle.time) return;
        const x = xScaleTime(new Date(candle.time).getTime());
        const isGreen = candle.close >= candle.open;
        const color = isGreen ? '#10b981' : '#ef4444';

        // Draw wick
        ctx.strokeStyle = color;
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(x, yScale(candle.high));
        ctx.lineTo(x, yScale(candle.low));
        ctx.stroke();

        // Draw body
        const openY = yScale(candle.open);
        const closeY = yScale(candle.close);
        const bodyTop = Math.min(openY, closeY);
        const bodyHeight = Math.max(Math.abs(closeY - openY), 1);

        ctx.fillStyle = color;
        ctx.fillRect(x - candleWidth / 2, bodyTop, candleWidth, bodyHeight);
      });
    }

    // Draw volume bars (if enabled and have data)
    if (showVolume && rawData.length > 0) {
      const volumeTop = padding.top + chartHeight + 10;
      const volumeChartHeight = volumeHeight - 20;
      
      // Calculate max volume for scaling
      const volumes = rawData.map(d => d.lowest_price_volume + d.highest_price_volume);
      const maxVolume = Math.max(...volumes, 1);
      
      // Draw volume separator line
      ctx.strokeStyle = '#374151';
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(padding.left, volumeTop - 5);
      ctx.lineTo(width - padding.right, volumeTop - 5);
      ctx.stroke();
      
      // Draw volume bars
      const barWidth = Math.max(1, Math.min(8, chartWidth / rawData.length * 0.8));
      
      rawData.forEach(d => {
        if (!d.time) return;
        const x = xScaleTime(new Date(d.time).getTime());
        const sellVol = d.lowest_price_volume || 0;
        const buyVol = d.highest_price_volume || 0;
        const totalVol = sellVol + buyVol;
        
        if (totalVol === 0) return;
        
        const barHeight = (totalVol / maxVolume) * volumeChartHeight;
        const barY = volumeTop + volumeChartHeight - barHeight;
        
        // Color based on which volume is higher
        ctx.fillStyle = sellVol >= buyVol ? 'rgba(16, 185, 129, 0.5)' : 'rgba(6, 182, 212, 0.5)';
        ctx.fillRect(x - barWidth / 2, barY, barWidth, barHeight);
      });
      
      // Volume label
      ctx.fillStyle = '#6b7280';
      ctx.font = '10px system-ui';
      ctx.textAlign = 'left';
      ctx.fillText('Volume', padding.left, volumeTop + 8);
    }

    // Draw time labels - evenly spaced across the time range
    ctx.fillStyle = '#6b7280';
    ctx.font = '11px system-ui';
    ctx.textAlign = 'center';
    const labelCount = 6;
    for (let i = 0; i < labelCount; i++) {
      const t = timeFrom + (i / (labelCount - 1)) * timeSpan;
      const time = new Date(t);
      const x = xScaleTime(t);
      const label = timeRange === '24h'
        ? time.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
        : time.toLocaleDateString([], { month: 'short', day: 'numeric' });
      ctx.fillText(label, x, height - 10);
    }

    // Draw legend (only for line chart) - shows which lines are visible
    if (chartMode === 'line') {
      ctx.font = '11px system-ui';
      ctx.textAlign = 'left';
      const legendY = 12;
      let legendX = padding.left + 5;
      
      // Sell price legend
      ctx.fillStyle = showSell ? '#10b981' : '#374151';
      ctx.fillRect(legendX, legendY - 6, 12, 3);
      ctx.fillStyle = showSell ? '#9ca3af' : '#4b5563';
      ctx.fillText('Sell', legendX + 16, legendY);
      legendX += 50;
      
      // Buy price legend
      ctx.fillStyle = showBuy ? '#06b6d4' : '#374151';
      ctx.fillRect(legendX, legendY - 6, 12, 3);
      ctx.fillStyle = showBuy ? '#9ca3af' : '#4b5563';
      ctx.fillText('Buy', legendX + 16, legendY);
      legendX += 45;
      
      // Average price legend
      ctx.fillStyle = showAvg ? '#fbbf24' : '#374151';
      ctx.fillRect(legendX, legendY - 6, 12, 3);
      ctx.fillStyle = showAvg ? '#9ca3af' : '#4b5563';
      ctx.fillText('Avg', legendX + 16, legendY);
    }

  }, [data, rawData, chartMode, timeRange, timeParams, showSell, showBuy, showAvg, showVolume]);

  // Handle mouse move for hover
  const handleMouseMove = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current;
    const container = containerRef.current;
    if (!canvas || !container) return;

    const rect = canvas.getBoundingClientRect();
    const x = e.clientX - rect.left;

    const padding = { top: 20, right: 60, bottom: 40, left: 10 };
    const chartWidth = rect.width - padding.left - padding.right;

    const chartData = chartMode === 'candle' ? data : rawData;
    if (chartData.length === 0) return;

    const index = Math.round(((x - padding.left) / chartWidth) * (chartData.length - 1));
    if (index >= 0 && index < chartData.length) {
      setHoveredPoint({
        x: e.clientX - rect.left,
        y: e.clientY - rect.top,
        data: chartData[index],
      });
    }
  }, [data, rawData, chartMode]);

  const handleMouseLeave = useCallback(() => {
    setHoveredPoint(null);
  }, []);

  // Redraw on data change or resize
  useEffect(() => {
    drawChart();

    const handleResize = () => drawChart();
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, [drawChart]);

  if (loading) {
    return (
      <div className="w-full h-full flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-emerald-500"></div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="w-full h-full flex items-center justify-center text-red-400">
        {error}
      </div>
    );
  }

  return (
    <div className="w-full h-full flex flex-col">
      {/* Chart controls */}
      <div className="flex justify-between items-center gap-2 mb-2">
        {/* Line toggles (only show for line chart) */}
        {chartMode === 'line' && (
          <div className="flex gap-1">
            <button
              onClick={() => setShowSell(!showSell)}
              className={`px-2 py-1 text-xs rounded flex items-center gap-1 transition-colors ${
                showSell ? 'bg-emerald-600/20 text-emerald-400 border border-emerald-600' : 'bg-gray-800 text-gray-500 border border-gray-700'
              }`}
            >
              <span className={`w-2 h-2 rounded-full ${showSell ? 'bg-emerald-400' : 'bg-gray-600'}`} />
              Sell
            </button>
            <button
              onClick={() => setShowBuy(!showBuy)}
              className={`px-2 py-1 text-xs rounded flex items-center gap-1 transition-colors ${
                showBuy ? 'bg-cyan-600/20 text-cyan-400 border border-cyan-600' : 'bg-gray-800 text-gray-500 border border-gray-700'
              }`}
            >
              <span className={`w-2 h-2 rounded-full ${showBuy ? 'bg-cyan-400' : 'bg-gray-600'}`} />
              Buy
            </button>
            <button
              onClick={() => setShowAvg(!showAvg)}
              className={`px-2 py-1 text-xs rounded flex items-center gap-1 transition-colors ${
                showAvg ? 'bg-amber-600/20 text-amber-400 border border-amber-600' : 'bg-gray-800 text-gray-500 border border-gray-700'
              }`}
            >
              <span className={`w-2 h-2 rounded-full ${showAvg ? 'bg-amber-400' : 'bg-gray-600'}`} />
              Avg
            </button>
            <button
              onClick={() => setShowVolume(!showVolume)}
              className={`px-2 py-1 text-xs rounded flex items-center gap-1 transition-colors ${
                showVolume ? 'bg-purple-600/20 text-purple-400 border border-purple-600' : 'bg-gray-800 text-gray-500 border border-gray-700'
              }`}
            >
              <span className={`w-2 h-2 rounded-full ${showVolume ? 'bg-purple-400' : 'bg-gray-600'}`} />
              Vol
            </button>
          </div>
        )}
        {chartMode === 'candle' && <div />}
        
        {/* Chart mode toggle */}
        <div className="flex gap-1">
          <button
            onClick={() => setChartMode('line')}
            className={`px-2 py-1 text-xs rounded ${
              chartMode === 'line' ? 'bg-emerald-600 text-white' : 'bg-gray-800 text-gray-400'
            }`}
          >
            Line
          </button>
          <button
            onClick={() => setChartMode('candle')}
            className={`px-2 py-1 text-xs rounded ${
              chartMode === 'candle' ? 'bg-emerald-600 text-white' : 'bg-gray-800 text-gray-400'
            }`}
          >
            Candles
          </button>
        </div>
      </div>

      {/* Chart */}
      <div ref={containerRef} className="flex-1 relative">
        <canvas
          ref={canvasRef}
          onMouseMove={handleMouseMove}
          onMouseLeave={handleMouseLeave}
          className="w-full h-full cursor-crosshair"
        />

        {/* Tooltip */}
        {hoveredPoint && (
          <div
            className="absolute pointer-events-none bg-gray-900 border border-gray-700 rounded-lg p-2 text-xs z-10"
            style={{
              left: Math.min(hoveredPoint.x + 10, (containerRef.current?.clientWidth || 300) - 150),
              top: Math.max(hoveredPoint.y - 80, 10),
            }}
          >
            <div className="text-gray-400 mb-1">
              {hoveredPoint.data?.time ? new Date(hoveredPoint.data.time).toLocaleString() : '-'}
            </div>
            {'open' in hoveredPoint.data ? (
              <>
                <div className="flex justify-between gap-4">
                  <span className="text-gray-500">O:</span>
                  <span>{hoveredPoint.data.open.toLocaleString()}</span>
                </div>
                <div className="flex justify-between gap-4">
                  <span className="text-gray-500">H:</span>
                  <span className="text-emerald-400">{hoveredPoint.data.high.toLocaleString()}</span>
                </div>
                <div className="flex justify-between gap-4">
                  <span className="text-gray-500">L:</span>
                  <span className="text-red-400">{hoveredPoint.data.low.toLocaleString()}</span>
                </div>
                <div className="flex justify-between gap-4">
                  <span className="text-gray-500">C:</span>
                  <span>{hoveredPoint.data.close.toLocaleString()}</span>
                </div>
                <div className="flex justify-between gap-4 border-t border-gray-700 mt-1 pt-1">
                  <span className="text-gray-500">Vol:</span>
                  <span className="text-cyan-400">{hoveredPoint.data.volume.toLocaleString()}</span>
                </div>
              </>
            ) : (
              <>
                <div className="flex justify-between gap-4">
                  <span className="text-emerald-400">Sell:</span>
                  <span>{(hoveredPoint.data as PriceSnapshot).lowest_sell_price?.toLocaleString() || '-'}</span>
                </div>
                <div className="flex justify-between gap-4">
                  <span className="text-cyan-400">Buy:</span>
                  <span>{(hoveredPoint.data as PriceSnapshot).highest_buy_price?.toLocaleString() || '-'}</span>
                </div>
                <div className="flex justify-between gap-4">
                  <span className="text-amber-400">Avg:</span>
                  <span>
                    {(() => {
                      const d = hoveredPoint.data as PriceSnapshot;
                      const sell = d.lowest_sell_price || 0;
                      const buy = d.highest_buy_price || 0;
                      if (sell > 0 && buy > 0) return Math.round((sell + buy) / 2).toLocaleString();
                      return (sell || buy || 0).toLocaleString();
                    })()}
                  </span>
                </div>
                <div className="flex justify-between gap-4 border-t border-gray-700 mt-1 pt-1">
                  <span className="text-purple-400">Sell Vol:</span>
                  <span>{(hoveredPoint.data as PriceSnapshot).lowest_price_volume?.toLocaleString() || '0'}</span>
                </div>
                <div className="flex justify-between gap-4">
                  <span className="text-purple-400">Buy Vol:</span>
                  <span>{(hoveredPoint.data as PriceSnapshot).highest_price_volume?.toLocaleString() || '0'}</span>
                </div>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function formatPrice(price: number): string {
  if (price >= 1000000) {
    return (price / 1000000).toFixed(1) + 'M';
  }
  if (price >= 1000) {
    return (price / 1000).toFixed(1) + 'K';
  }
  return price.toFixed(0);
}

