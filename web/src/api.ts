import type { UserData, PlayerData } from './types';

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

