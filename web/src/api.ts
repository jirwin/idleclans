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

