import { useEffect, useState, useCallback, useMemo, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import type { MarketOverview, MarketItem, ItemSummary, PriceChange, PriceSnapshot } from '../api';
import {
  fetchMarketOverview,
  fetchMarketStats,
  searchMarketItems,
  fetchMarketItem,
  fetchAllMarketItems,
  fetchPriceHistory,
} from '../api';
import { PriceChart } from '../components/PriceChart';

// Calculate volatility as coefficient of variation (std dev / mean * 100)
function calculateVolatility(prices: number[]): number {
  if (prices.length < 2) return 0;
  const mean = prices.reduce((a, b) => a + b, 0) / prices.length;
  if (mean === 0) return 0;
  const variance = prices.reduce((sum, p) => sum + Math.pow(p - mean, 2), 0) / prices.length;
  const stdDev = Math.sqrt(variance);
  return (stdDev / mean) * 100; // Coefficient of variation as percentage
}

// Mini sparkline component for table rows - lazy loads when visible
function Sparkline({ 
  itemId, 
  onVolatilityCalculated 
}: { 
  itemId: number;
  onVolatilityCalculated?: (itemId: number, volatility: number) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [data, setData] = useState<PriceSnapshot[]>([]);
  const [loading, setLoading] = useState(true);
  const [isVisible, setIsVisible] = useState(false);
  const hasLoadedRef = useRef(false);

  // Use IntersectionObserver to detect when sparkline becomes visible
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const observer = new IntersectionObserver(
      (entries) => {
        const [entry] = entries;
        if (entry.isIntersecting && !hasLoadedRef.current) {
          setIsVisible(true);
          hasLoadedRef.current = true;
        }
      },
      {
        rootMargin: '100px', // Start loading a bit before visible
        threshold: 0,
      }
    );

    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  // Only fetch data when visible
  useEffect(() => {
    if (!isVisible) return;

    let cancelled = false;
    
    async function loadData() {
      try {
        const now = new Date();
        const from = new Date(now.getTime() - 24 * 60 * 60 * 1000);
        const response = await fetchPriceHistory(itemId, {
          from: from.toISOString(),
          to: now.toISOString(),
          limit: 100,
        });
        if (!cancelled && response?.history) {
          setData(response.history);
          
          // Calculate and report volatility
          const prices = response.history.map(d => d.lowest_sell_price).filter(p => p > 0);
          const volatility = calculateVolatility(prices);
          onVolatilityCalculated?.(itemId, volatility);
        }
      } catch (err) {
        // Silently fail - sparkline is optional
        onVolatilityCalculated?.(itemId, 0);
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    
    loadData();
    
    // Auto-refresh sparkline every 2 minutes
    const interval = setInterval(loadData, 2 * 60 * 1000);
    
    return () => { 
      cancelled = true; 
      clearInterval(interval);
    };
  }, [itemId, isVisible, onVolatilityCalculated]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || data.length < 2) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const width = canvas.width;
    const height = canvas.height;
    
    // Clear
    ctx.clearRect(0, 0, width, height);

    // Get prices
    const prices = data.map(d => d.lowest_sell_price).filter(p => p > 0);
    if (prices.length < 2) return;

    const minPrice = Math.min(...prices);
    const maxPrice = Math.max(...prices);
    const range = maxPrice - minPrice || 1;

    // Determine color based on trend
    const firstPrice = prices[0];
    const lastPrice = prices[prices.length - 1];
    const isUp = lastPrice >= firstPrice;
    const color = isUp ? '#10b981' : '#ef4444';

    // Draw line
    ctx.beginPath();
    ctx.strokeStyle = color;
    ctx.lineWidth = 1.5;

    prices.forEach((price, i) => {
      const x = (i / (prices.length - 1)) * width;
      const y = height - ((price - minPrice) / range) * height;
      if (i === 0) {
        ctx.moveTo(x, y);
      } else {
        ctx.lineTo(x, y);
      }
    });
    ctx.stroke();

    // Fill gradient
    ctx.lineTo(width, height);
    ctx.lineTo(0, height);
    ctx.closePath();
    const gradient = ctx.createLinearGradient(0, 0, 0, height);
    gradient.addColorStop(0, isUp ? 'rgba(16, 185, 129, 0.3)' : 'rgba(239, 68, 68, 0.3)');
    gradient.addColorStop(1, 'transparent');
    ctx.fillStyle = gradient;
    ctx.fill();
  }, [data]);

  // Show placeholder until visible
  if (!isVisible || loading) {
    return <div ref={containerRef} className="w-20 h-8 bg-gray-800/30 rounded animate-pulse" />;
  }

  if (data.length < 2) {
    return <div ref={containerRef} className="w-20 h-8 text-gray-600 text-xs flex items-center justify-center">—</div>;
  }

  return (
    <div ref={containerRef}>
      <canvas ref={canvasRef} width={80} height={32} className="w-20 h-8" />
    </div>
  );
}

export function Market() {
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedItemId = searchParams.get('item') ? parseInt(searchParams.get('item')!) : null;

  const [overview, setOverview] = useState<MarketOverview | null>(null);
  const [items, setItems] = useState<MarketItem[]>([]);
  const [selectedItem, setSelectedItem] = useState<ItemSummary | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<MarketItem[]>([]);
  const [overviewLoading, setOverviewLoading] = useState(true);
  const [itemsLoading, setItemsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [activeTab, setActiveTab] = useState<'gainers' | 'losers' | 'volume'>('gainers');

  // Load overview data
  const loadOverview = useCallback(async () => {
    try {
      const [overviewData] = await Promise.all([
        fetchMarketOverview(),
        fetchMarketStats(),
      ]);
      setOverview(overviewData);
      setLastUpdated(new Date());
      setError(null);
    } catch (err) {
      setError('Failed to load market data');
      console.error(err);
    } finally {
      setOverviewLoading(false);
    }
  }, []);

  // Load all items for browsing
  const loadItems = useCallback(async () => {
    try {
      const itemsData = await fetchAllMarketItems();
      setItems(itemsData);
      setLastUpdated(new Date());
    } catch (err) {
      console.error('Failed to load items:', err);
    } finally {
      setItemsLoading(false);
    }
  }, []);

  // Load selected item details
  const loadSelectedItem = useCallback(async (itemId: number) => {
    try {
      const itemData = await fetchMarketItem(itemId);
      setSelectedItem(itemData);
    } catch (err) {
      console.error('Failed to load item:', err);
      setSelectedItem(null);
    }
  }, []);

  // Search items
  const handleSearch = useCallback(async (query: string) => {
    setSearchQuery(query);
    if (query.length < 2) {
      setSearchResults([]);
      return;
    }
    try {
      const results = await searchMarketItems(query, 10);
      setSearchResults(results);
    } catch (err) {
      console.error('Search failed:', err);
    }
  }, []);

  // Select an item
  const selectItem = useCallback((item: MarketItem) => {
    setSearchParams({ item: item.id.toString() });
    setSearchQuery('');
    setSearchResults([]);
  }, [setSearchParams]);

  // Initial load
  useEffect(() => {
    loadOverview();
    loadItems();
  }, [loadOverview, loadItems]);

  // Load selected item when URL changes
  useEffect(() => {
    if (selectedItemId) {
      loadSelectedItem(selectedItemId);
    } else {
      setSelectedItem(null);
    }
  }, [selectedItemId, loadSelectedItem]);

  // Auto-refresh data every 2 minutes (matches collector interval)
  useEffect(() => {
    const REFRESH_INTERVAL = 2 * 60 * 1000; // 2 minutes
    
    const interval = setInterval(() => {
      loadOverview();
      loadItems();
    }, REFRESH_INTERVAL);
    
    return () => clearInterval(interval);
  }, [loadOverview, loadItems]);

  // Auto-refresh selected item every 30 seconds when viewing
  useEffect(() => {
    if (!selectedItemId) return;
    
    const interval = setInterval(() => {
      loadSelectedItem(selectedItemId);
    }, 30 * 1000); // 30 seconds
    
    return () => clearInterval(interval);
  }, [selectedItemId, loadSelectedItem]);

  // Filter items by category
  const categories = useMemo(() => {
    const cats = new Set(items.map(i => i.category));
    return Array.from(cats).sort();
  }, [items]);

  const [selectedCategory, setSelectedCategory] = useState<string | null>(null);
  const filteredItems = useMemo(() => {
    if (!selectedCategory) return items;
    return items.filter(i => i.category === selectedCategory);
  }, [items, selectedCategory]);

  // Get current movers list based on active tab
  const currentMovers = useMemo(() => {
    if (!overview) return [];
    switch (activeTab) {
      case 'gainers': return overview.top_gainers || [];
      case 'losers': return overview.top_losers || [];
      case 'volume': return overview.most_traded || [];
      default: return [];
    }
  }, [overview, activeTab]);

  return (
    <div className="min-h-screen bg-[#0a0f1a] text-gray-100">
      {/* Header */}
      <header className="border-b border-gray-800 bg-[#0d1321]">
        <div className="max-w-7xl mx-auto px-4 py-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-4">
              <h1 className="text-2xl font-bold bg-gradient-to-r from-emerald-400 to-cyan-400 bg-clip-text text-transparent">
                IdleClans Market
              </h1>
              {lastUpdated && (
                <span className="text-xs text-gray-500 flex items-center gap-2">
                  <span className="w-2 h-2 bg-emerald-500 rounded-full animate-pulse" title="Live updates active" />
                  Updated {lastUpdated.toLocaleTimeString()}
                </span>
              )}
            </div>
            
            {/* Search */}
            <div className="relative w-80">
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => handleSearch(e.target.value)}
                placeholder="Search items..."
                className="w-full bg-[#1a2234] border border-gray-700 rounded-lg px-4 py-2 text-sm focus:outline-none focus:border-emerald-500 transition-colors"
              />
              {searchResults.length > 0 && (
                <div className="absolute top-full left-0 right-0 mt-1 bg-[#1a2234] border border-gray-700 rounded-lg shadow-xl z-50 max-h-64 overflow-y-auto">
                  {searchResults.map(item => (
                    <button
                      key={item.id}
                      onClick={() => selectItem(item)}
                      className="w-full text-left px-4 py-2 hover:bg-gray-700/50 transition-colors flex justify-between items-center"
                    >
                      <span className="font-medium">{item.display_name}</span>
                      <span className="text-xs text-gray-500">{item.category}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      </header>

      <div className="max-w-7xl mx-auto px-4 py-6">
        {error && (
          <div className="mb-4 p-4 bg-red-900/20 border border-red-800 rounded-lg text-red-400">
            {error}
          </div>
        )}

        {/* Overview Stats */}
        <div className="grid grid-cols-4 gap-4 mb-6">
          <StatCard
            label="Total Items"
            value={overview?.total_items?.toLocaleString() ?? '0'}
            icon="📦"
            loading={overviewLoading}
          />
          <StatCard
            label="Active Items"
            value={overview?.active_items?.toLocaleString() ?? '0'}
            icon="📈"
            loading={overviewLoading}
          />
          <StatCard
            label="Top Gainer"
            value={overview?.top_gainers?.[0]?.change_percent != null 
              ? `${overview.top_gainers[0].change_percent.toFixed(1)}%` 
              : '-'}
            subValue={overview?.top_gainers?.[0]?.display_name}
            positive
            icon="🚀"
            loading={overviewLoading}
          />
          <StatCard
            label="Top Loser"
            value={overview?.top_losers?.[0]?.change_percent != null 
              ? `${overview.top_losers[0].change_percent.toFixed(1)}%` 
              : '-'}
            subValue={overview?.top_losers?.[0]?.display_name}
            negative
            icon="📉"
            loading={overviewLoading}
          />
        </div>

        <div className="grid grid-cols-3 gap-6">
          {/* Left column: Movers */}
          <div className="col-span-1 space-y-6">
            {/* Tab selector */}
            <div className="bg-[#0d1321] rounded-xl border border-gray-800 p-4">
              <div className="flex gap-2 mb-4">
                <TabButton
                  active={activeTab === 'gainers'}
                  onClick={() => setActiveTab('gainers')}
                >
                  🚀 Gainers
                </TabButton>
                <TabButton
                  active={activeTab === 'losers'}
                  onClick={() => setActiveTab('losers')}
                >
                  📉 Losers
                </TabButton>
                <TabButton
                  active={activeTab === 'volume'}
                  onClick={() => setActiveTab('volume')}
                >
                  📊 Volume
                </TabButton>
              </div>

              <div className="space-y-2">
                {overviewLoading ? (
                  // Loading skeleton for movers
                  Array.from({ length: 5 }).map((_, idx) => (
                    <div key={idx} className="flex items-center gap-3 p-2 rounded-lg">
                      <div className="w-5 h-4 bg-gray-800 rounded animate-pulse" />
                      <div className="flex-1">
                        <div className="h-4 w-32 bg-gray-800 rounded animate-pulse mb-1" />
                        <div className="h-3 w-20 bg-gray-800 rounded animate-pulse" />
                      </div>
                      <div className="h-4 w-12 bg-gray-800 rounded animate-pulse" />
                    </div>
                  ))
                ) : currentMovers.length === 0 ? (
                  <p className="text-gray-500 text-center py-4">No data available</p>
                ) : (
                  currentMovers.map((item, idx) => (
                    <MoverRow
                      key={item.item_id}
                      item={item}
                      rank={idx + 1}
                      showVolume={activeTab === 'volume'}
                      onClick={() => selectItem({ 
                        id: item.item_id, 
                        name_id: item.name_id, 
                        display_name: item.display_name, 
                        category: '', 
                        lowest_sell_price: item.current_price,
                        lowest_price_volume: 0,
                        highest_buy_price: 0,
                        highest_price_volume: 0,
                        spread: 0,
                        spread_percent: 0,
                        last_updated: '' 
                      })}
                    />
                  ))
                )}
              </div>
            </div>

            {/* Category filter */}
            <div className="bg-[#0d1321] rounded-xl border border-gray-800 p-4">
              <h3 className="text-sm font-semibold text-gray-400 mb-3">Categories</h3>
              <div className="flex flex-wrap gap-2">
                <button
                  onClick={() => setSelectedCategory(null)}
                  className={`px-3 py-1 rounded-full text-xs font-medium transition-colors ${
                    selectedCategory === null
                      ? 'bg-emerald-600 text-white'
                      : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                  }`}
                >
                  All
                </button>
                {categories.map(cat => (
                  <button
                    key={cat}
                    onClick={() => setSelectedCategory(cat)}
                    className={`px-3 py-1 rounded-full text-xs font-medium transition-colors capitalize ${
                      selectedCategory === cat
                        ? 'bg-emerald-600 text-white'
                        : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                    }`}
                  >
                    {cat.replace(/_/g, ' ')}
                  </button>
                ))}
              </div>
            </div>
          </div>

          {/* Right column: Selected item or item list */}
          <div className="col-span-2">
            {selectedItem ? (
              <ItemDetail
                item={selectedItem}
                onClose={() => setSearchParams({})}
              />
            ) : (
              <ItemDataTable
                items={filteredItems}
                onSelect={selectItem}
                loading={itemsLoading}
              />
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// Stat card component
function StatCard({
  label,
  value,
  subValue,
  positive,
  negative,
  icon,
  loading,
}: {
  label: string;
  value: string;
  subValue?: string;
  positive?: boolean;
  negative?: boolean;
  icon: string;
  loading?: boolean;
}) {
  return (
    <div className="bg-[#0d1321] rounded-xl border border-gray-800 p-4">
      <div className="flex items-center gap-2 mb-2">
        <span className="text-xl">{icon}</span>
        <span className="text-xs text-gray-500 uppercase tracking-wider">{label}</span>
      </div>
      {loading ? (
        <div className="h-8 w-20 bg-gray-800 rounded animate-pulse" />
      ) : (
        <div className={`text-2xl font-bold ${positive ? 'text-emerald-400' : negative ? 'text-red-400' : 'text-white'}`}>
          {value}
        </div>
      )}
      {loading ? (
        <div className="h-3 w-24 bg-gray-800 rounded animate-pulse mt-1" />
      ) : subValue ? (
        <div className="text-xs text-gray-500 truncate mt-1">{subValue}</div>
      ) : null}
    </div>
  );
}

// Tab button component
function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${
        active
          ? 'bg-emerald-600 text-white'
          : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
      }`}
    >
      {children}
    </button>
  );
}

// Mover row component
function MoverRow({
  item,
  rank,
  showVolume,
  onClick,
}: {
  item: PriceChange;
  rank: number;
  showVolume: boolean;
  onClick: () => void;
}) {
  const isPositive = item.change_percent >= 0;

  return (
    <button
      onClick={onClick}
      className="w-full flex items-center gap-3 p-2 rounded-lg hover:bg-gray-800/50 transition-colors text-left"
    >
      <span className="text-gray-500 text-sm w-5">{rank}</span>
      <div className="flex-1 min-w-0">
        <div className="font-medium truncate">{item.display_name}</div>
        <div className="text-xs text-gray-500">
          {item.current_price.toLocaleString()} gold
        </div>
      </div>
      <div className="text-right">
        {showVolume ? (
          <div className="text-sm text-cyan-400">{item.volume.toLocaleString()}</div>
        ) : (
          <div className={`text-sm font-medium ${isPositive ? 'text-emerald-400' : 'text-red-400'}`}>
            {isPositive ? '+' : ''}{item.change_percent.toFixed(1)}%
          </div>
        )}
      </div>
    </button>
  );
}

// Sort configuration type - includes volatility as special key
type SortKey = keyof MarketItem | 'volatility';
type SortConfig = {
  key: SortKey | null;
  direction: 'asc' | 'desc';
};

// Item data table component with sorting and filtering
function ItemDataTable({
  items,
  onSelect,
  loading,
}: {
  items: MarketItem[];
  onSelect: (item: MarketItem) => void;
  loading?: boolean;
}) {
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(25);
  const [sortConfig, setSortConfig] = useState<SortConfig>({ key: 'lowest_sell_price', direction: 'desc' });
  const [filterText, setFilterText] = useState('');
  
  // Track volatility per item (calculated by Sparkline components)
  const [volatilityMap, setVolatilityMap] = useState<Map<number, number>>(new Map());
  
  // Callback for sparklines to report volatility
  const handleVolatilityCalculated = useCallback((itemId: number, volatility: number) => {
    setVolatilityMap(prev => {
      const next = new Map(prev);
      next.set(itemId, volatility);
      return next;
    });
  }, []);

  // Filter items
  const filteredItems = useMemo(() => {
    if (!filterText) return items;
    const lower = filterText.toLowerCase();
    return items.filter(item => 
      item.display_name.toLowerCase().includes(lower) ||
      item.category.toLowerCase().includes(lower)
    );
  }, [items, filterText]);

  // Sort items
  const sortedItems = useMemo(() => {
    if (!sortConfig.key) return filteredItems;
    
    return [...filteredItems].sort((a, b) => {
      // Special handling for volatility sorting
      if (sortConfig.key === 'volatility') {
        const aVol = volatilityMap.get(a.id) ?? 0;
        const bVol = volatilityMap.get(b.id) ?? 0;
        return sortConfig.direction === 'asc' ? aVol - bVol : bVol - aVol;
      }
      
      const aVal = a[sortConfig.key as keyof MarketItem];
      const bVal = b[sortConfig.key as keyof MarketItem];
      
      if (typeof aVal === 'string' && typeof bVal === 'string') {
        return sortConfig.direction === 'asc' 
          ? aVal.localeCompare(bVal)
          : bVal.localeCompare(aVal);
      }
      
      if (typeof aVal === 'number' && typeof bVal === 'number') {
        return sortConfig.direction === 'asc' ? aVal - bVal : bVal - aVal;
      }
      
      return 0;
    });
  }, [filteredItems, sortConfig, volatilityMap]);

  // Paginate
  const totalPages = Math.ceil(sortedItems.length / pageSize);
  const pagedItems = sortedItems.slice(page * pageSize, (page + 1) * pageSize);

  // Handle sort
  const handleSort = (key: SortKey) => {
    setSortConfig(prev => ({
      key,
      direction: prev.key === key && prev.direction === 'asc' ? 'desc' : 'asc'
    }));
    setPage(0);
  };

  // Sort indicator
  const SortIndicator = ({ column }: { column: SortKey }) => {
    if (sortConfig.key !== column) return <span className="text-gray-600 ml-1">⇅</span>;
    return <span className="text-emerald-400 ml-1">{sortConfig.direction === 'asc' ? '↑' : '↓'}</span>;
  };

  // Format gold value
  const formatGold = (value: number) => {
    if (value >= 1000000) return `${(value / 1000000).toFixed(1)}M`;
    if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
    return value.toLocaleString();
  };

  return (
    <div className="bg-[#0d1321] rounded-xl border border-gray-800 overflow-hidden">
      {/* Header with filter */}
      <div className="p-4 border-b border-gray-800 flex items-center justify-between gap-4">
        <div className="flex items-center gap-4">
          <h3 className="text-lg font-semibold">Market Items</h3>
          <span className="text-sm text-gray-500">{filteredItems.length} items</span>
        </div>
        <div className="flex items-center gap-4">
          <input
            type="text"
            value={filterText}
            onChange={(e) => { setFilterText(e.target.value); setPage(0); }}
            placeholder="Filter items..."
            className="bg-[#1a2234] border border-gray-700 rounded-lg px-3 py-1.5 text-sm w-48 focus:outline-none focus:border-emerald-500"
          />
          <select
            value={pageSize}
            onChange={(e) => { setPageSize(Number(e.target.value)); setPage(0); }}
            className="bg-[#1a2234] border border-gray-700 rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:border-emerald-500"
          >
            <option value={10}>10</option>
            <option value={25}>25</option>
            <option value={50}>50</option>
            <option value={100}>100</option>
          </select>
        </div>
      </div>

      {/* Table */}
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="bg-gray-900/50">
            <tr>
              <th 
                className="text-left px-4 py-3 font-medium text-gray-400 cursor-pointer hover:text-white transition-colors"
                onClick={() => handleSort('display_name')}
              >
                Item <SortIndicator column="display_name" />
              </th>
              <th 
                className="text-center px-2 py-3 font-medium text-gray-400 cursor-pointer hover:text-white transition-colors"
                onClick={() => handleSort('volatility')}
                title="Sort by 24h volatility"
              >
                24h <SortIndicator column="volatility" />
              </th>
              <th 
                className="text-right px-4 py-3 font-medium text-gray-400 cursor-pointer hover:text-white transition-colors"
                onClick={() => handleSort('lowest_sell_price')}
              >
                Sell Price <SortIndicator column="lowest_sell_price" />
              </th>
              <th 
                className="text-right px-4 py-3 font-medium text-gray-400 cursor-pointer hover:text-white transition-colors"
                onClick={() => handleSort('highest_buy_price')}
              >
                Buy Price <SortIndicator column="highest_buy_price" />
              </th>
              <th 
                className="text-right px-4 py-3 font-medium text-gray-400 cursor-pointer hover:text-white transition-colors"
                onClick={() => handleSort('spread')}
              >
                Spread <SortIndicator column="spread" />
              </th>
              <th 
                className="text-right px-4 py-3 font-medium text-gray-400 cursor-pointer hover:text-white transition-colors"
                onClick={() => handleSort('spread_percent')}
              >
                Spread % <SortIndicator column="spread_percent" />
              </th>
              <th 
                className="text-left px-4 py-3 font-medium text-gray-400 cursor-pointer hover:text-white transition-colors"
                onClick={() => handleSort('category')}
              >
                Category <SortIndicator column="category" />
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {loading ? (
              // Loading skeleton rows
              Array.from({ length: 10 }).map((_, idx) => (
                <tr key={idx}>
                  <td className="px-4 py-3"><div className="h-4 w-32 bg-gray-800 rounded animate-pulse" /></td>
                  <td className="px-2 py-3"><div className="w-20 h-8 bg-gray-800/30 rounded animate-pulse" /></td>
                  <td className="px-4 py-3 text-right"><div className="h-4 w-16 bg-gray-800 rounded animate-pulse ml-auto" /></td>
                  <td className="px-4 py-3 text-right"><div className="h-4 w-16 bg-gray-800 rounded animate-pulse ml-auto" /></td>
                  <td className="px-4 py-3 text-right"><div className="h-4 w-14 bg-gray-800 rounded animate-pulse ml-auto" /></td>
                  <td className="px-4 py-3 text-right"><div className="h-4 w-12 bg-gray-800 rounded animate-pulse ml-auto" /></td>
                  <td className="px-4 py-3"><div className="h-4 w-20 bg-gray-800 rounded animate-pulse" /></td>
                </tr>
              ))
            ) : pagedItems.length === 0 ? (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-gray-500">
                  No items found
                </td>
              </tr>
            ) : (
              pagedItems.map(item => (
                <tr 
                  key={item.id}
                  onClick={() => onSelect(item)}
                  className="hover:bg-gray-800/50 cursor-pointer transition-colors"
                >
                  <td className="px-4 py-3 font-medium">{item.display_name}</td>
                  <td className="px-2 py-3">
                    <Sparkline itemId={item.id} onVolatilityCalculated={handleVolatilityCalculated} />
                  </td>
                  <td className="px-4 py-3 text-right text-emerald-400 font-mono">
                    {item.lowest_sell_price > 0 ? formatGold(item.lowest_sell_price) : '-'}
                  </td>
                  <td className="px-4 py-3 text-right text-cyan-400 font-mono">
                    {item.highest_buy_price > 0 ? formatGold(item.highest_buy_price) : '-'}
                  </td>
                  <td className="px-4 py-3 text-right font-mono">
                    {item.spread > 0 ? formatGold(item.spread) : '-'}
                  </td>
                  <td className="px-4 py-3 text-right font-mono">
                    {item.spread_percent > 0 ? `${item.spread_percent.toFixed(1)}%` : '-'}
                  </td>
                  <td className="px-4 py-3 text-gray-400 capitalize">{item.category.replace(/_/g, ' ')}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="p-4 border-t border-gray-800 flex items-center justify-between">
          <div className="text-sm text-gray-500">
            Showing {page * pageSize + 1} - {Math.min((page + 1) * pageSize, sortedItems.length)} of {sortedItems.length}
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setPage(0)}
              disabled={page === 0}
              className="px-2 py-1 rounded bg-gray-800 text-gray-400 disabled:opacity-50 hover:bg-gray-700 disabled:hover:bg-gray-800"
            >
              ««
            </button>
            <button
              onClick={() => setPage(p => Math.max(0, p - 1))}
              disabled={page === 0}
              className="px-3 py-1 rounded bg-gray-800 text-gray-400 disabled:opacity-50 hover:bg-gray-700 disabled:hover:bg-gray-800"
            >
              ←
            </button>
            <span className="text-sm text-gray-400 px-2">
              {page + 1} / {totalPages}
            </span>
            <button
              onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}
              disabled={page === totalPages - 1}
              className="px-3 py-1 rounded bg-gray-800 text-gray-400 disabled:opacity-50 hover:bg-gray-700 disabled:hover:bg-gray-800"
            >
              →
            </button>
            <button
              onClick={() => setPage(totalPages - 1)}
              disabled={page === totalPages - 1}
              className="px-2 py-1 rounded bg-gray-800 text-gray-400 disabled:opacity-50 hover:bg-gray-700 disabled:hover:bg-gray-800"
            >
              »»
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// Item detail component
function ItemDetail({
  item,
  onClose,
}: {
  item: ItemSummary;
  onClose: () => void;
}) {
  const [timeRange, setTimeRange] = useState<'1h' | '24h' | '7d' | '30d'>('24h');

  const priceChange = item?.change_24h;
  const isPositive = priceChange && priceChange.change >= 0;

  if (!item?.item) {
    return (
      <div className="bg-[#0d1321] rounded-xl border border-gray-800 p-8 text-center text-gray-400">
        Loading item details...
      </div>
    );
  }

  return (
    <div className="bg-[#0d1321] rounded-xl border border-gray-800 overflow-hidden">
      {/* Header */}
      <div className="p-4 border-b border-gray-800 flex items-center justify-between">
        <div>
          <h2 className="text-xl font-bold">{item.item.display_name || 'Unknown Item'}</h2>
          <div className="flex items-center gap-3">
            <span className="text-sm text-gray-500 capitalize">{(item.item.category || 'other').replace(/_/g, ' ')}</span>
            {item.current_price?.time && (
              <span className="text-xs text-gray-600">
                Updated {new Date(item.current_price.time).toLocaleString()}
              </span>
            )}
          </div>
        </div>
        <button
          onClick={onClose}
          className="p-2 rounded-lg hover:bg-gray-800 transition-colors text-gray-400 hover:text-white"
        >
          ✕
        </button>
      </div>

      {/* Price info */}
      <div className="p-4 border-b border-gray-800 grid grid-cols-4 gap-4">
        <div>
          <div className="text-xs text-gray-500 mb-1">Current Price</div>
          <div className="text-2xl font-bold text-white">
            {item.current_price?.lowest_sell_price?.toLocaleString() ?? '-'}
          </div>
          {priceChange && (
            <div className={`text-sm ${isPositive ? 'text-emerald-400' : 'text-red-400'}`}>
              {isPositive ? '+' : ''}{priceChange.change?.toLocaleString() ?? 0} ({priceChange.change_percent?.toFixed(2) ?? '0.00'}%)
            </div>
          )}
        </div>
        <div>
          <div className="text-xs text-gray-500 mb-1">Buy Price</div>
          <div className="text-lg font-semibold text-cyan-400">
            {item.current_price?.highest_buy_price?.toLocaleString() ?? '-'}
          </div>
        </div>
        <div>
          <div className="text-xs text-gray-500 mb-1">Spread</div>
          <div className="text-lg font-semibold">
            {item.spread?.toLocaleString() ?? '-'}
            <span className="text-sm text-gray-500 ml-1">({item.spread_percent?.toFixed(1) ?? '0.0'}%)</span>
          </div>
        </div>
        <div>
          <div className="text-xs text-gray-500 mb-1">Volatility (24h)</div>
          <div className="text-lg font-semibold">
            {item.volatility?.toFixed(2) ?? '0.00'}%
          </div>
        </div>
      </div>

      {/* Time range selector */}
      <div className="p-4 border-b border-gray-800 flex gap-2">
        {(['1h', '24h', '7d', '30d'] as const).map(range => (
          <button
            key={range}
            onClick={() => setTimeRange(range)}
            className={`px-3 py-1 rounded text-sm font-medium transition-colors ${
              timeRange === range
                ? 'bg-emerald-600 text-white'
                : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
            }`}
          >
            {range}
          </button>
        ))}
      </div>

      {/* Chart */}
      <div className="p-4" style={{ height: '400px' }}>
        <PriceChart itemId={item.item.id} timeRange={timeRange} />
      </div>

      {/* Volume info */}
      <div className="p-4 border-t border-gray-800 grid grid-cols-2 gap-4">
        <div>
          <div className="text-xs text-gray-500 mb-1">Sell Volume</div>
          <div className="text-lg font-semibold text-emerald-400">
            {item.current_price?.lowest_price_volume?.toLocaleString() ?? '-'}
          </div>
        </div>
        <div>
          <div className="text-xs text-gray-500 mb-1">Buy Volume</div>
          <div className="text-lg font-semibold text-cyan-400">
            {item.current_price?.highest_price_volume?.toLocaleString() ?? '-'}
          </div>
        </div>
      </div>
    </div>
  );
}

