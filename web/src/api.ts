import type { UserData, PlayerData, ClanBossData, ClanKeysData, PlanData, PartySession, PartySummary } from './types';

const API_BASE = '/api';

export async function fetchUserData(): Promise<UserData> {
  const res = await fetch(`${API_BASE}/me`, {
    credentials: 'include',
  });
  
  if (res.status === 401) {
    throw new Error('Unauthorized');
  }
  
  if (!res.ok) {
    throw new Error(`Failed to fetch user data: ${res.statusText}`);
  }
  
  return res.json();
}

export async function updateQuest(
  playerName: string,
  boss: string,
  requiredKills: number
): Promise<void> {
  const res = await fetch(`${API_BASE}/quests/${encodeURIComponent(playerName)}/${boss}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ required_kills: requiredKills }),
  });
  
  if (!res.ok) {
    throw new Error(`Failed to update quest: ${res.statusText}`);
  }
}

export async function updateKeys(playerName: string, keyType: string, count: number): Promise<void> {
  const res = await fetch(`${API_BASE}/keys/${encodeURIComponent(playerName)}/${keyType}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ count }),
  });
  
  if (!res.ok) {
    throw new Error(`Failed to update keys: ${res.statusText}`);
  }
}

export async function logout(): Promise<void> {
  await fetch(`${API_BASE}/auth/logout`, {
    method: 'POST',
    credentials: 'include',
  });
}

export interface RegisterResponse {
  success: boolean;
  message?: string;
  error?: string;
}

export async function registerPlayer(playerName: string): Promise<RegisterResponse> {
  const res = await fetch(`${API_BASE}/auth/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ player_name: playerName }),
  });
  
  return res.json();
}

export interface AltResponse {
  status?: string;
  message?: string;
  error?: string;
}

export async function addAlt(playerName: string): Promise<AltResponse> {
  const res = await fetch(`${API_BASE}/alts`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ player_name: playerName }),
  });
  
  return res.json();
}

export async function removeAlt(playerName: string): Promise<AltResponse> {
  const res = await fetch(`${API_BASE}/alts/${encodeURIComponent(playerName)}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  
  return res.json();
}

// Clan view API functions

export async function fetchClanBosses(): Promise<ClanBossData> {
  const res = await fetch(`${API_BASE}/clan/bosses`, {
    credentials: 'include',
  });
  
  if (res.status === 401) {
    throw new Error('Unauthorized');
  }
  
  if (!res.ok) {
    throw new Error(`Failed to fetch clan bosses: ${res.statusText}`);
  }
  
  return res.json();
}

export async function fetchClanKeys(): Promise<ClanKeysData> {
  const res = await fetch(`${API_BASE}/clan/keys`, {
    credentials: 'include',
  });
  
  if (res.status === 401) {
    throw new Error('Unauthorized');
  }
  
  if (!res.ok) {
    throw new Error(`Failed to fetch clan keys: ${res.statusText}`);
  }
  
  return res.json();
}

export async function fetchClanPlan(onlinePlayers?: string[]): Promise<PlanData> {
  const res = await fetch(`${API_BASE}/clan/plan`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ online_players: onlinePlayers || [] }),
  });
  
  if (res.status === 401) {
    throw new Error('Unauthorized');
  }
  
  if (!res.ok) {
    throw new Error(`Failed to fetch plan: ${res.statusText}`);
  }
  
  return res.json();
}

export async function fetchClanPlayers(): Promise<string[]> {
  const res = await fetch(`${API_BASE}/clan/players`, {
    credentials: 'include',
  });
  
  if (res.status === 401) {
    throw new Error('Unauthorized');
  }
  
  if (!res.ok) {
    throw new Error(`Failed to fetch players: ${res.statusText}`);
  }
  
  const data = await res.json();
  return data.players || [];
}

export async function sendPlanToDiscord(players: string[], noPing: boolean = false): Promise<void> {
  const res = await fetch(`${API_BASE}/clan/plan/send`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ players, no_ping: noPing }),
  });
  
  if (res.status === 401) {
    throw new Error('Unauthorized');
  }
  
  if (res.status === 503) {
    throw new Error('Discord integration not configured');
  }
  
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `Failed to send to Discord: ${res.statusText}`);
  }
}

// Admin API functions

export async function checkAdminAccess(): Promise<boolean> {
  try {
    const res = await fetch(`${API_BASE}/admin/check`);
    if (!res.ok) return false;
    const data = await res.json();
    return data?.admin === true;
  } catch {
    return false;
  }
}

export async function fetchAllPlayers(): Promise<PlayerData[]> {
  const res = await fetch(`${API_BASE}/players`);
  
  if (!res.ok) {
    throw new Error(`Failed to fetch players: ${res.statusText}`);
  }
  
  return res.json();
}

export async function fetchPlayer(discordId: string): Promise<PlayerData> {
  const res = await fetch(`${API_BASE}/players/${discordId}`);
  
  if (!res.ok) {
    throw new Error(`Failed to fetch player: ${res.statusText}`);
  }
  
  return res.json();
}

export async function adminUpdateQuest(
  discordId: string,
  playerName: string,
  boss: string,
  requiredKills: number
): Promise<void> {
  const res = await fetch(
    `${API_BASE}/players/${discordId}/quests/${encodeURIComponent(playerName)}/${boss}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ required_kills: requiredKills }),
    }
  );
  
  if (!res.ok) {
    throw new Error(`Failed to update quest: ${res.statusText}`);
  }
}

export async function adminUpdateKeys(
  discordId: string,
  playerName: string,
  keyType: string,
  count: number
): Promise<void> {
  const res = await fetch(
    `${API_BASE}/players/${discordId}/keys/${encodeURIComponent(playerName)}/${keyType}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ count }),
    }
  );
  
  if (!res.ok) {
    throw new Error(`Failed to update keys: ${res.statusText}`);
  }
}

export async function adminUnregisterPlayer(discordId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/players/${discordId}/unregister`, {
    method: 'POST',
  });
  
  if (!res.ok) {
    throw new Error(`Failed to unregister player: ${res.statusText}`);
  }
}

export async function adminDeletePlayer(discordId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/players/${discordId}`, {
    method: 'DELETE',
  });
  
  if (!res.ok) {
    throw new Error(`Failed to delete player: ${res.statusText}`);
  }
}

// Screenshot analysis API functions

export interface AnalyzedBoss {
  name: string;
  kills: number;
}

export interface AnalyzeQuestsResponse {
  bosses: AnalyzedBoss[];
  applied: boolean;
  error?: string;
}

export interface AnalyzedKey {
  type: string;
  count: number;
}

export interface AnalyzeKeysResponse {
  keys: AnalyzedKey[];
  applied: boolean;
  error?: string;
}

export async function analyzeQuestsScreenshot(image: File): Promise<AnalyzeQuestsResponse> {
  const formData = new FormData();
  formData.append('image', image);

  const res = await fetch(`${API_BASE}/analyze/quests`, {
    method: 'POST',
    credentials: 'include',
    body: formData,
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  if (res.status === 503) {
    throw new Error('Image analysis not configured');
  }

  const data = await res.json();
  
  if (!res.ok) {
    throw new Error(data.error || `Failed to analyze screenshot: ${res.statusText}`);
  }

  return data;
}

export async function analyzeKeysScreenshot(image: File): Promise<AnalyzeKeysResponse> {
  const formData = new FormData();
  formData.append('image', image);

  const res = await fetch(`${API_BASE}/analyze/keys`, {
    method: 'POST',
    credentials: 'include',
    body: formData,
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  if (res.status === 503) {
    throw new Error('Image analysis not configured');
  }

  const data = await res.json();
  
  if (!res.ok) {
    throw new Error(data.error || `Failed to analyze screenshot: ${res.statusText}`);
  }

  return data;
}

// Party API functions

export async function getUserParties(): Promise<PartySummary[]> {
  const res = await fetch(`${API_BASE}/parties`, {
    credentials: 'include',
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  if (!res.ok) {
    throw new Error(`Failed to get parties: ${res.statusText}`);
  }

  return res.json();
}

export async function createParty(players: string[]): Promise<{ id: string }> {
  const res = await fetch(`${API_BASE}/parties`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ players }),
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  if (!res.ok) {
    throw new Error(`Failed to create party: ${res.statusText}`);
  }

  return res.json();
}

export async function getParty(partyId: string): Promise<PartySession> {
  const res = await fetch(`${API_BASE}/parties/${partyId}`, {
    credentials: 'include',
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  if (res.status === 404) {
    throw new Error('Party not found');
  }

  if (res.status === 403) {
    throw new Error('You do not have access to this party');
  }

  if (!res.ok) {
    throw new Error(`Failed to get party: ${res.statusText}`);
  }

  return res.json();
}

export async function startPartyStep(partyId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/parties/${partyId}/start`, {
    method: 'POST',
    credentials: 'include',
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `Failed to start step: ${res.statusText}`);
  }
}

export interface UpdateKillsResult {
  kills: number;
  conflict?: boolean;
  actual_kills?: number;
}

export async function updatePartyKills(
  partyId: string,
  kills: number,
  delta: boolean = false,
  expectedKills?: number
): Promise<UpdateKillsResult> {
  const body: Record<string, unknown> = { kills, delta };
  if (expectedKills !== undefined) {
    body.expected_kills = expectedKills;
  }

  const res = await fetch(`${API_BASE}/parties/${partyId}/kills`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify(body),
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  // Handle conflict (optimistic concurrency failure)
  if (res.status === 409) {
    const data = await res.json();
    return {
      kills: data.actual_kills,
      conflict: true,
      actual_kills: data.actual_kills,
    };
  }

  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `Failed to update kills: ${res.statusText}`);
  }

  return res.json();
}

export async function updatePartyKeys(partyId: string, keysUsed: number): Promise<void> {
  const res = await fetch(`${API_BASE}/parties/${partyId}/keys`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ keys_used: keysUsed }),
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `Failed to update keys: ${res.statusText}`);
  }
}

export async function nextPartyStep(partyId: string): Promise<{ current_step_index: number }> {
  const res = await fetch(`${API_BASE}/parties/${partyId}/next-step`, {
    method: 'POST',
    credentials: 'include',
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `Failed to advance step: ${res.statusText}`);
  }

  return res.json();
}

export async function endParty(partyId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/parties/${partyId}/end`, {
    method: 'POST',
    credentials: 'include',
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `Failed to end party: ${res.statusText}`);
  }
}

// Market API functions

export interface MarketItem {
  id: number;
  name_id: string;
  display_name: string;
  category: string;
  lowest_sell_price: number;
  lowest_price_volume: number;
  highest_buy_price: number;
  highest_price_volume: number;
  spread: number;
  spread_percent: number;
  last_updated: string;
}

export interface PriceSnapshot {
  time: string;
  item_id: number;
  lowest_sell_price: number;
  lowest_price_volume: number;
  highest_buy_price: number;
  highest_price_volume: number;
}

export interface PriceChange24h {
  previous_price: number;
  current_price: number;
  change: number;
  change_percent: number;
}

export interface ItemSummary {
  item: MarketItem;
  current_price: PriceSnapshot | null;
  change_24h: PriceChange24h | null;
  volatility: number;
  spread: number;
  spread_percent: number;
}

export interface PriceChange {
  item_id: number;
  name_id: string;
  display_name: string;
  current_price: number;
  previous_price: number;
  price_change: number;
  change_percent: number;
  volume: number;
}

export interface MarketOverview {
  total_items: number;
  active_items: number;
  top_gainers: PriceChange[];
  top_losers: PriceChange[];
  most_traded: PriceChange[];
  last_updated: string;
}

export interface OHLC {
  time: string;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

export interface PriceHistoryResponse {
  item: MarketItem;
  history: PriceSnapshot[];
  ohlc?: OHLC[];
}

export interface DailyAggregate {
  date: string;
  item_id: number;
  open_price: number;
  high_price: number;
  low_price: number;
  close_price: number;
  avg_price: number;
  total_sell_volume: number;
  total_buy_volume: number;
  sample_count: number;
}

export interface SpreadAnalysis {
  current_spread: number;
  current_spread_pct: number;
  avg_spread_24h: number;
  min_spread_24h: number;
  max_spread_24h: number;
  spread_volatility: number;
}

export interface VolumeAnalysis {
  current_sell_volume: number;
  current_buy_volume: number;
  avg_volume_24h: number;
  volume_change: number;
  is_high_volume: boolean;
}

export interface CollectorStats {
  last_collection_time: string;
  last_collection_count: string;
  total_items: number;
  total_snapshots: number;
  collection_interval: string;
}

const MARKET_API_BASE = '/api/market';

export async function fetchMarketOverview(): Promise<MarketOverview> {
  const res = await fetch(`${MARKET_API_BASE}/overview`);
  if (!res.ok) {
    throw new Error(`Failed to fetch market overview: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchMarketStats(): Promise<CollectorStats> {
  const res = await fetch(`${MARKET_API_BASE}/stats`);
  if (!res.ok) {
    throw new Error(`Failed to fetch market stats: ${res.statusText}`);
  }
  return res.json();
}

export async function searchMarketItems(query: string, limit = 20): Promise<MarketItem[]> {
  const res = await fetch(`${MARKET_API_BASE}/items/search?q=${encodeURIComponent(query)}&limit=${limit}`);
  if (!res.ok) {
    throw new Error(`Failed to search items: ${res.statusText}`);
  }
  return res.json();
}

export interface PaginatedMarketItems {
  items: MarketItem[];
  total: number;
}

export async function fetchAllMarketItems(): Promise<MarketItem[]> {
  const res = await fetch(`${MARKET_API_BASE}/items`);
  if (!res.ok) {
    throw new Error(`Failed to fetch items: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchMarketItemsPaginated(page: number, limit: number): Promise<PaginatedMarketItems> {
  const res = await fetch(`${MARKET_API_BASE}/items?page=${page}&limit=${limit}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch items: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchMarketItem(itemId: number): Promise<ItemSummary> {
  const res = await fetch(`${MARKET_API_BASE}/items/${itemId}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch item: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchMarketItemByName(nameId: string): Promise<ItemSummary> {
  const res = await fetch(`${MARKET_API_BASE}/item-by-name/${encodeURIComponent(nameId)}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch item: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchPriceHistory(
  itemId: number,
  options?: {
    from?: string;
    to?: string;
    limit?: number;
    interval?: number;
    ohlc?: boolean;
  }
): Promise<PriceHistoryResponse> {
  const params = new URLSearchParams();
  if (options?.from) params.set('from', options.from);
  if (options?.to) params.set('to', options.to);
  if (options?.limit) params.set('limit', options.limit.toString());
  if (options?.interval) params.set('interval', options.interval.toString());
  if (options?.ohlc) params.set('ohlc', 'true');

  const res = await fetch(`${MARKET_API_BASE}/items/${itemId}/history?${params}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch price history: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchDailyPrices(
  itemId: number,
  from?: string,
  to?: string
): Promise<{ item: MarketItem; daily: DailyAggregate[] }> {
  const params = new URLSearchParams();
  if (from) params.set('from', from);
  if (to) params.set('to', to);

  const res = await fetch(`${MARKET_API_BASE}/items/${itemId}/daily?${params}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch daily prices: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchOHLC(
  itemId: number,
  options?: {
    from?: string;
    to?: string;
    interval?: number;
  }
): Promise<OHLC[]> {
  const params = new URLSearchParams();
  if (options?.from) params.set('from', options.from);
  if (options?.to) params.set('to', options.to);
  if (options?.interval) params.set('interval', options.interval.toString());

  const res = await fetch(`${MARKET_API_BASE}/items/${itemId}/ohlc?${params}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch OHLC data: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchTopMovers(
  type: 'gainers' | 'losers' = 'gainers',
  hours = 24,
  limit = 10
): Promise<PriceChange[]> {
  const res = await fetch(`${MARKET_API_BASE}/movers?type=${type}&hours=${hours}&limit=${limit}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch top movers: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchMostTraded(hours = 24, limit = 10): Promise<PriceChange[]> {
  const res = await fetch(`${MARKET_API_BASE}/most-traded?hours=${hours}&limit=${limit}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch most traded: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchSpreadAnalysis(itemId: number): Promise<SpreadAnalysis> {
  const res = await fetch(`${MARKET_API_BASE}/items/${itemId}/spread`);
  if (!res.ok) {
    throw new Error(`Failed to fetch spread analysis: ${res.statusText}`);
  }
  return res.json();
}

export async function fetchVolumeAnalysis(itemId: number): Promise<VolumeAnalysis> {
  const res = await fetch(`${MARKET_API_BASE}/items/${itemId}/volume`);
  if (!res.ok) {
    throw new Error(`Failed to fetch volume analysis: ${res.statusText}`);
  }
  return res.json();
}

