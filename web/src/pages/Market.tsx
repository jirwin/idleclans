import { useEffect, useState, useCallback, useMemo, useRef } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import type { MarketOverview, MarketItem, ItemSummary, PriceChange, PriceSnapshot, MarketWatchWithItem } from '../api';
import type { UserData } from '../types';
import {
  fetchMarketOverview,
  fetchMarketStats,
  searchMarketItems,
  fetchMarketItem,
  fetchAllMarketItems,
  fetchPriceHistory,
  fetchUserData,
  fetchMarketWatches,
  createMarketWatch,
  deleteMarketWatch,
} from '../api';
import { PriceChart } from '../components/PriceChart';
import { useSSE } from '../hooks/useSSE';

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
  const navigate = useNavigate();
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
  
  // Optional authentication state
  const [user, setUser] = useState<UserData | null>(null);
  const [userLoading, setUserLoading] = useState(true);

  // Watch state
  const [watches, setWatches] = useState<MarketWatchWithItem[]>([]);
  const [watchesLoading, setWatchesLoading] = useState(false);
  const [showWatchModal, setShowWatchModal] = useState(false);

  // Try to load user data (optional - don't redirect on 401)
  const loadUser = useCallback(async () => {
    try {
      const userData = await fetchUserData();
      setUser(userData);
    } catch {
      // Not logged in or error - that's fine, market is public
      setUser(null);
    } finally {
      setUserLoading(false);
    }
  }, []);

  // Load user's watches
  const loadWatches = useCallback(async () => {
    if (!user) return;
    // Don't show loading indicator for background refreshes
    try {
      const watchData = await fetchMarketWatches();
      // Only update state if data has changed
      setWatches(prev => {
        const newJson = JSON.stringify(watchData);
        const prevJson = JSON.stringify(prev);
        return newJson !== prevJson ? watchData : prev;
      });
    } catch {
      // Silently fail - watches are optional
    } finally {
      setWatchesLoading(false);
    }
  }, [user]);

  // Delete a watch
  const handleDeleteWatch = useCallback(async (watchId: number) => {
    try {
      await deleteMarketWatch(watchId);
      setWatches(prev => prev.filter(w => w.id !== watchId));
    } catch (err) {
      console.error('Failed to delete watch:', err);
    }
  }, []);

  // Load user on mount
  useEffect(() => {
    loadUser();
  }, [loadUser]);

  // Load watches when user is available
  useEffect(() => {
    if (user) {
      loadWatches();
    } else {
      setWatches([]);
    }
  }, [user, loadWatches]);

  // Load overview data
  const loadOverview = useCallback(async () => {
    try {
      const [overviewData] = await Promise.all([
        fetchMarketOverview(),
        fetchMarketStats(),
      ]);
      // Only update state if data has changed (compare JSON to avoid reference issues)
      setOverview(prev => {
        const newJson = JSON.stringify(overviewData);
        const prevJson = JSON.stringify(prev);
        return newJson !== prevJson ? overviewData : prev;
      });
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
      // Only update state if data has changed
      setItems(prev => {
        const newJson = JSON.stringify(itemsData);
        const prevJson = JSON.stringify(prev);
        return newJson !== prevJson ? itemsData : prev;
      });
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
      // Only update state if data has changed
      setSelectedItem(prev => {
        const newJson = JSON.stringify(itemData);
        const prevJson = JSON.stringify(prev);
        return newJson !== prevJson ? itemData : prev;
      });
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

  // Fallback auto-refresh every 5 minutes (SSE is primary, this is backup if SSE disconnects)
  useEffect(() => {
    const REFRESH_INTERVAL = 5 * 60 * 1000; // 5 minutes
    
    const interval = setInterval(() => {
      loadOverview();
      loadItems();
      if (user) {
        loadWatches();
      }
    }, REFRESH_INTERVAL);
    
    return () => clearInterval(interval);
  }, [loadOverview, loadItems, loadWatches, user]);

  // Use SSE to refresh watches and market data when updates arrive
  useSSE({
    onUpdate: useCallback((eventType?: string) => {
      if (eventType === 'market') {
        // Refresh overview and items when market data updates
        loadOverview();
        loadItems();
        // Refresh watches if user is logged in
        if (user) {
          loadWatches();
        }
        // Refresh selected item if viewing one
        if (selectedItemId) {
          loadSelectedItem(selectedItemId);
        }
      } else if (eventType?.startsWith('market:item:')) {
        // Specific item updated (e.g., trade volume fetched)
        const itemId = parseInt(eventType.split(':')[2], 10);
        if (selectedItemId === itemId) {
          loadSelectedItem(itemId);
        }
      }
    }, [loadOverview, loadItems, loadWatches, loadSelectedItem, user, selectedItemId]),
    enabled: true,
  });

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
            
            <div className="flex items-center gap-3">
              {/* Search */}
              <div className="relative w-64">
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
              
              {/* User section */}
              {!userLoading && (
                user ? (
                  <div className="flex items-center gap-3">
                    <button
                      onClick={() => navigate('/dashboard')}
                      className="px-3 py-2 text-sm text-gray-400 hover:text-white border border-gray-600 hover:border-gray-500 rounded-lg transition-colors"
                    >
                      My Quests
                    </button>
                    <div className="flex items-center gap-2 px-3 py-1.5 bg-[#1a2234] rounded-lg border border-gray-700">
                      {user.avatar && (
                        <img
                          src={`https://cdn.discordapp.com/avatars/${user.discord_id}/${user.avatar}.png`}
                          alt=""
                          className="w-6 h-6 rounded-full"
                        />
                      )}
                      <span className="text-sm text-gray-300">{user.username}</span>
                    </div>
                  </div>
                ) : (
                  <a
                    href="/api/auth/discord"
                    className="px-4 py-2 text-sm font-medium bg-[#5865F2] hover:bg-[#4752C4] text-white rounded-lg transition-colors flex items-center gap-2"
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
                      <path d="M20.317 4.37a19.791 19.791 0 0 0-4.885-1.515.074.074 0 0 0-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 0 0-5.487 0 12.64 12.64 0 0 0-.617-1.25.077.077 0 0 0-.079-.037A19.736 19.736 0 0 0 3.677 4.37a.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 0 0 .031.057 19.9 19.9 0 0 0 5.993 3.03.078.078 0 0 0 .084-.028 14.09 14.09 0 0 0 1.226-1.994.076.076 0 0 0-.041-.106 13.107 13.107 0 0 1-1.872-.892.077.077 0 0 1-.008-.128 10.2 10.2 0 0 0 .372-.292.074.074 0 0 1 .077-.01c3.928 1.793 8.18 1.793 12.062 0a.074.074 0 0 1 .078.01c.12.098.246.198.373.292a.077.077 0 0 1-.006.127 12.299 12.299 0 0 1-1.873.892.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 0 0 .084.028 19.839 19.839 0 0 0 6.002-3.03.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 0 0-.031-.03zM8.02 15.33c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.956-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.956 2.418-2.157 2.418zm7.975 0c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.946 2.418-2.157 2.418z"/>
                    </svg>
                    Sign In
                  </a>
                )
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

        {/* Watch Dashboard - only shown when user is logged in and has watches */}
        {user && (watches.length > 0 || watchesLoading) && (
          <WatchDashboard
            watches={watches}
            loading={watchesLoading}
            onDelete={handleDeleteWatch}
            onItemClick={(itemId) => setSearchParams({ item: itemId.toString() })}
          />
        )}

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
                user={user}
                onWatchClick={() => setShowWatchModal(true)}
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

      {/* Watch Modal */}
      {showWatchModal && selectedItem && (
        <WatchModal
          item={selectedItem}
          onClose={() => setShowWatchModal(false)}
          onCreated={loadWatches}
        />
      )}
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
  user,
  onWatchClick,
}: {
  item: ItemSummary;
  onClose: () => void;
  user?: UserData | null;
  onWatchClick?: () => void;
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
            {item.trade_volume_1d !== undefined && item.trade_volume_1d > 0 && (
              <span className="text-xs text-purple-400 bg-purple-900/30 px-2 py-0.5 rounded-full">
                {item.trade_volume_1d.toLocaleString()} traded/24h
              </span>
            )}
            {item.current_price?.time && (
              <span className="text-xs text-gray-600">
                Updated {new Date(item.current_price.time).toLocaleString()}
              </span>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {user && onWatchClick && (
            <button
              onClick={onWatchClick}
              className="px-3 py-2 rounded-lg bg-amber-600 hover:bg-amber-500 transition-colors text-white text-sm font-medium flex items-center gap-1"
              title="Create price watch"
            >
              <span>👁</span>
              <span>Watch</span>
            </button>
          )}
          <button
            onClick={onClose}
            className="p-2 rounded-lg hover:bg-gray-800 transition-colors text-gray-400 hover:text-white"
          >
            ✕
          </button>
        </div>
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

// Watch Modal component
function WatchModal({
  item,
  onClose,
  onCreated,
}: {
  item: ItemSummary;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [watchType, setWatchType] = useState<'buy' | 'sell'>('sell');
  const [threshold, setThreshold] = useState<string>('');
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Pre-fill threshold with current price
  useEffect(() => {
    if (watchType === 'sell' && item.current_price?.lowest_sell_price) {
      setThreshold(item.current_price.lowest_sell_price.toString());
    } else if (watchType === 'buy' && item.current_price?.highest_buy_price) {
      setThreshold(item.current_price.highest_buy_price.toString());
    }
  }, [watchType, item.current_price]);

  const handleCreate = async () => {
    const thresholdNum = parseInt(threshold);
    if (!thresholdNum || thresholdNum <= 0) {
      setError('Please enter a valid threshold');
      return;
    }

    setCreating(true);
    setError(null);

    try {
      await createMarketWatch(item.item.id, watchType, thresholdNum);
      onCreated();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create watch');
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50" onClick={onClose}>
      <div 
        className="bg-[#0d1321] rounded-xl border border-gray-700 p-6 w-full max-w-md"
        onClick={e => e.stopPropagation()}
      >
        <h2 className="text-xl font-bold mb-4">Create Price Watch</h2>
        <p className="text-gray-400 text-sm mb-4">
          Get notified in Discord when the price threshold is met for <span className="text-white font-medium">{item.item.display_name}</span>.
        </p>

        {/* Watch type selector */}
        <div className="mb-4">
          <label className="block text-sm text-gray-400 mb-2">Watch Type</label>
          <div className="flex gap-2">
            <button
              onClick={() => setWatchType('sell')}
              className={`flex-1 px-4 py-3 rounded-lg border transition-colors ${
                watchType === 'sell'
                  ? 'bg-emerald-600 border-emerald-500 text-white'
                  : 'bg-gray-800 border-gray-700 text-gray-400 hover:bg-gray-700'
              }`}
            >
              <div className="font-medium">Sell Price</div>
              <div className="text-xs opacity-75">Alert when ≤ threshold</div>
            </button>
            <button
              onClick={() => setWatchType('buy')}
              className={`flex-1 px-4 py-3 rounded-lg border transition-colors ${
                watchType === 'buy'
                  ? 'bg-cyan-600 border-cyan-500 text-white'
                  : 'bg-gray-800 border-gray-700 text-gray-400 hover:bg-gray-700'
              }`}
            >
              <div className="font-medium">Buy Price</div>
              <div className="text-xs opacity-75">Alert when ≥ threshold</div>
            </button>
          </div>
        </div>

        {/* Current price reference */}
        <div className="mb-4 p-3 bg-gray-800/50 rounded-lg">
          <div className="text-xs text-gray-500 mb-1">Current {watchType === 'sell' ? 'Sell' : 'Buy'} Price</div>
          <div className="text-lg font-semibold">
            {watchType === 'sell'
              ? item.current_price?.lowest_sell_price?.toLocaleString() ?? '-'
              : item.current_price?.highest_buy_price?.toLocaleString() ?? '-'}
          </div>
        </div>

        {/* Threshold input */}
        <div className="mb-4">
          <label className="block text-sm text-gray-400 mb-2">
            Price Threshold
            <span className="text-gray-600 ml-2">
              ({watchType === 'sell' ? 'alert when ≤ this' : 'alert when ≥ this'})
            </span>
          </label>
          <input
            type="number"
            value={threshold}
            onChange={e => setThreshold(e.target.value)}
            className="w-full px-4 py-2 bg-gray-800 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-amber-500"
            placeholder="Enter price threshold"
            min="1"
          />
        </div>

        {error && (
          <div className="mb-4 p-3 bg-red-900/30 border border-red-800 rounded-lg text-red-400 text-sm">
            {error}
          </div>
        )}

        {/* Actions */}
        <div className="flex gap-3">
          <button
            onClick={onClose}
            className="flex-1 px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg transition-colors text-gray-300"
          >
            Cancel
          </button>
          <button
            onClick={handleCreate}
            disabled={creating || !threshold}
            className="flex-1 px-4 py-2 bg-amber-600 hover:bg-amber-500 disabled:bg-gray-700 disabled:text-gray-500 rounded-lg transition-colors text-white font-medium"
          >
            {creating ? 'Creating...' : 'Create Watch'}
          </button>
        </div>
      </div>
    </div>
  );
}

// Watch Dashboard component
function WatchDashboard({
  watches,
  loading,
  onDelete,
  onItemClick,
}: {
  watches: MarketWatchWithItem[];
  loading: boolean;
  onDelete: (watchId: number) => void;
  onItemClick: (itemId: number) => void;
}) {
  if (loading) {
    return (
      <div className="bg-[#0d1321] rounded-xl border border-gray-800 p-6 mb-6">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-bold flex items-center gap-2">
            <span>👁</span> Your Market Watches
          </h2>
        </div>
        <div className="animate-pulse space-y-3">
          <div className="h-12 bg-gray-800 rounded"></div>
          <div className="h-12 bg-gray-800 rounded"></div>
        </div>
      </div>
    );
  }

  if (watches.length === 0) {
    return null; // Don't show dashboard if no watches
  }

  return (
    <div className="bg-[#0d1321] rounded-xl border border-gray-800 p-6 mb-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-bold flex items-center gap-2">
          <span>👁</span> Your Market Watches
          <span className="text-sm font-normal text-gray-500">({watches.length}/10)</span>
        </h2>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="text-left text-xs text-gray-500 uppercase tracking-wider border-b border-gray-800">
              <th className="pb-3 font-medium">Item</th>
              <th className="pb-3 font-medium">Type</th>
              <th className="pb-3 font-medium">Threshold</th>
              <th className="pb-3 font-medium">Current Price</th>
              <th className="pb-3 font-medium">Status</th>
              <th className="pb-3 font-medium"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {watches.map(watch => {
              const currentPrice = watch.watch_type === 'sell' 
                ? watch.current_sell_price 
                : watch.current_buy_price;
              
              const isClose = watch.watch_type === 'sell'
                ? currentPrice > 0 && currentPrice <= watch.threshold * 1.1
                : currentPrice > 0 && currentPrice >= watch.threshold * 0.9;

              return (
                <tr key={watch.id} className="hover:bg-gray-800/30 transition-colors">
                  <td className="py-3">
                    <button
                      onClick={() => onItemClick(watch.item_id)}
                      className="text-left hover:text-amber-400 transition-colors"
                    >
                      <div className="font-medium">{watch.item_display_name}</div>
                      <div className="text-xs text-gray-500">{watch.item_name_id}</div>
                    </button>
                  </td>
                  <td className="py-3">
                    <span className={`px-2 py-1 rounded text-xs font-medium ${
                      watch.watch_type === 'sell' 
                        ? 'bg-emerald-900/30 text-emerald-400' 
                        : 'bg-cyan-900/30 text-cyan-400'
                    }`}>
                      {watch.watch_type === 'sell' ? 'Sell ≤' : 'Buy ≥'}
                    </span>
                  </td>
                  <td className="py-3 font-mono">
                    {watch.threshold.toLocaleString()}
                  </td>
                  <td className="py-3">
                    <span className={`font-mono ${isClose && !watch.triggered ? 'text-amber-400' : ''}`}>
                      {currentPrice > 0 ? currentPrice.toLocaleString() : '-'}
                    </span>
                  </td>
                  <td className="py-3">
                    {watch.triggered ? (
                      <div>
                        <span className="px-2 py-1 rounded text-xs font-medium bg-amber-900/30 text-amber-400">
                          Triggered
                        </span>
                        {watch.triggered_at && (
                          <div className="text-xs text-gray-500 mt-1">
                            {new Date(watch.triggered_at).toLocaleString()}
                          </div>
                        )}
                      </div>
                    ) : (
                      <span className="px-2 py-1 rounded text-xs font-medium bg-gray-800 text-gray-400">
                        Active
                      </span>
                    )}
                  </td>
                  <td className="py-3 text-right">
                    <button
                      onClick={() => onDelete(watch.id)}
                      className="p-2 rounded hover:bg-red-900/30 text-gray-500 hover:text-red-400 transition-colors"
                      title="Delete watch"
                    >
                      🗑
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

