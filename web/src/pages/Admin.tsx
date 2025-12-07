import { useEffect, useState, useCallback } from 'react';
import type { PlayerData } from '../types';
import { BOSSES, KEY_TYPES } from '../types';
import { fetchAllPlayers, adminUpdateQuest, adminUpdateKeys, checkAdminAccess, adminUnregisterPlayer, adminDeletePlayer } from '../api';
import { useSSE } from '../hooks/useSSE';

export function Admin() {
  const [players, setPlayers] = useState<PlayerData[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [accessDenied, setAccessDenied] = useState(false);
  const [selectedPlayer, setSelectedPlayer] = useState<PlayerData | null>(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [lastRefresh, setLastRefresh] = useState<Date>(new Date());
  const [isRefreshing, setIsRefreshing] = useState(false);

  const loadPlayers = useCallback(async (showRefreshIndicator = false) => {
    if (showRefreshIndicator) {
      setIsRefreshing(true);
    }
    try {
      // First verify we have admin access
      const hasAccess = await checkAdminAccess();
      if (!hasAccess) {
        setAccessDenied(true);
        setLoading(false);
        return;
      }
      
      const data = await fetchAllPlayers();
      setPlayers(data || []);
      setError(null);
      setLastRefresh(new Date());
      
      // Update selected player if it exists
      if (selectedPlayer) {
        const updated = (data || []).find(
          (p) => p.discord_id === selectedPlayer.discord_id && p.player_name === selectedPlayer.player_name
        );
        if (updated) setSelectedPlayer(updated);
      }
    } catch (err) {
      console.error('Admin load error:', err);
      setError('Failed to load players. Make sure you are accessing the admin port.');
    } finally {
      setLoading(false);
      setIsRefreshing(false);
    }
  }, [selectedPlayer]);

  // Use SSE for live updates
  useSSE({
    onUpdate: loadPlayers,
    enabled: !loading && !accessDenied,
  });

  // Initial load
  useEffect(() => {
    loadPlayers();
  }, []);

  // Refresh when window regains focus (fallback)
  useEffect(() => {
    const handleFocus = () => {
      loadPlayers();
    };

    window.addEventListener('focus', handleFocus);
    return () => window.removeEventListener('focus', handleFocus);
  }, [loadPlayers]);

  const handleManualRefresh = () => {
    loadPlayers(true);
  };

  const handleUpdateQuest = async (discordId: string, playerName: string, boss: string, kills: number) => {
    await adminUpdateQuest(discordId, playerName, boss, kills);
    await loadPlayers();
    // Refresh selected player
    if (selectedPlayer?.discord_id === discordId && selectedPlayer?.player_name === playerName) {
      const updated = players.find((p) => p.discord_id === discordId && p.player_name === playerName);
      if (updated) setSelectedPlayer(updated);
    }
  };

  const handleUpdateKeys = async (discordId: string, playerName: string, keyType: string, count: number) => {
    await adminUpdateKeys(discordId, playerName, keyType, count);
    await loadPlayers();
  };

  const filteredPlayers = players.filter(
    (p) =>
      p.player_name.toLowerCase().includes(searchTerm.toLowerCase()) ||
      p.discord_id.includes(searchTerm)
  );

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="flex flex-col items-center gap-4">
          <div className="w-12 h-12 border-4 border-red-500 border-t-transparent rounded-full animate-spin" />
          <p className="text-gray-400">Loading players...</p>
        </div>
      </div>
    );
  }

  if (accessDenied) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center max-w-md">
          <div className="text-6xl mb-4">🚫</div>
          <h1 className="text-2xl font-bold text-red-400 mb-2">Access Denied</h1>
          <p className="text-gray-400">
            Admin interface is only available on the internal admin port.
            This page cannot be accessed from the public internet.
          </p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <p className="text-red-400 mb-4">{error}</p>
          <button
            onClick={() => loadPlayers()}
            className="px-4 py-2 bg-red-600 hover:bg-red-700 text-white rounded-lg transition-colors"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen p-4 md:p-8">
      <div className="max-w-7xl mx-auto">
        {/* Header */}
        <header className="mb-8">
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-gradient-to-br from-red-600 to-red-900 flex items-center justify-center">
                <span className="text-xl">⚙️</span>
              </div>
              <div>
                <h1 className="text-2xl font-bold text-white">Admin Panel</h1>
                <p className="text-sm text-gray-400">
                  Manage all player quests and keys
                </p>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <p className="text-xs text-gray-500 hidden sm:block">
                Updated: {lastRefresh.toLocaleTimeString()}
              </p>
              <button
                onClick={handleManualRefresh}
                disabled={isRefreshing}
                className="p-2 text-gray-400 hover:text-red-400 border border-gray-600 hover:border-red-500 rounded-lg transition-colors disabled:opacity-50"
                title="Refresh data"
              >
                <svg
                  className={`w-5 h-5 ${isRefreshing ? 'animate-spin' : ''}`}
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
                  />
                </svg>
              </button>
            </div>
          </div>
          <div className="mt-4 p-3 bg-red-900/20 border border-red-700/50 rounded-lg">
            <p className="text-sm text-red-300 flex items-center gap-2">
              <span>⚠️</span>
              This admin panel should only be accessible on internal networks.
            </p>
          </div>
        </header>

        <div className="grid gap-6 lg:grid-cols-3">
          {/* Player List */}
          <div className="lg:col-span-1">
            <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
              <div className="p-4 border-b border-[var(--color-border)]">
                <h2 className="font-semibold text-white mb-3">Players ({players.length})</h2>
                <input
                  type="text"
                  placeholder="Search players..."
                  value={searchTerm}
                  onChange={(e) => setSearchTerm(e.target.value)}
                  className="w-full px-3 py-2 text-sm bg-[var(--color-bg-input)] border border-[var(--color-border)] rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-violet-500"
                />
              </div>
              <div className="max-h-[600px] overflow-y-auto">
                {filteredPlayers.length === 0 ? (
                  <div className="p-4 text-center text-gray-500">No players found</div>
                ) : (
                  filteredPlayers.map((player) => (
                    <button
                      key={`${player.discord_id}-${player.player_name}`}
                      onClick={() => setSelectedPlayer(player)}
                      className={`w-full px-4 py-3 text-left border-b border-[var(--color-border)] hover:bg-[var(--color-bg-input)] transition-colors ${
                        selectedPlayer?.discord_id === player.discord_id && selectedPlayer?.player_name === player.player_name
                          ? 'bg-violet-900/20 border-l-2 border-l-violet-500'
                          : ''
                      }`}
                    >
                      <div className="font-medium text-white flex items-center gap-2">
                        {player.player_name}
                        {player.is_alt && (
                          <span className="text-xs px-1.5 py-0.5 bg-amber-900/50 text-amber-400 rounded">ALT</span>
                        )}
                      </div>
                      <div className="text-xs text-gray-500 mt-1">
                        {player.is_alt ? (
                          <span>Main: {player.owner_name}</span>
                        ) : (
                          <>
                            ID: {player.discord_id}
                            {player.alts.length > 0 && (
                              <span className="ml-2">+{player.alts.length} alts</span>
                            )}
                          </>
                        )}
                      </div>
                    </button>
                  ))
                )}
              </div>
            </div>
          </div>

          {/* Player Details */}
          <div className="lg:col-span-2">
            {selectedPlayer ? (
              <PlayerDetails
                player={selectedPlayer}
                onUpdateQuest={handleUpdateQuest}
                onUpdateKeys={handleUpdateKeys}
                onUnregister={async () => {
                  if (!selectedPlayer.is_alt && confirm(`Unregister ${selectedPlayer.player_name}? They will need to re-register with their character name.`)) {
                    await adminUnregisterPlayer(selectedPlayer.discord_id);
                    setSelectedPlayer(null);
                    await loadPlayers();
                  }
                }}
                onDelete={async () => {
                  if (!selectedPlayer.is_alt && confirm(`DELETE ${selectedPlayer.player_name} and ALL their data (quests, keys, alts)? This cannot be undone!`)) {
                    await adminDeletePlayer(selectedPlayer.discord_id);
                    setSelectedPlayer(null);
                    await loadPlayers();
                  }
                }}
                onRefresh={loadPlayers}
              />
            ) : (
              <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-8 text-center">
                <div className="text-4xl mb-4">👈</div>
                <p className="text-gray-400">Select a player to view and edit their data</p>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

interface PlayerDetailsProps {
  player: PlayerData;
  onUpdateQuest: (discordId: string, playerName: string, boss: string, kills: number) => Promise<void>;
  onUpdateKeys: (discordId: string, playerName: string, keyType: string, count: number) => Promise<void>;
  onUnregister: () => Promise<void>;
  onDelete: () => Promise<void>;
  onRefresh: () => Promise<void>;
}

function PlayerDetails({ player, onUpdateQuest, onUpdateKeys, onUnregister, onDelete, onRefresh }: PlayerDetailsProps) {
  const [editingQuest, setEditingQuest] = useState<string | null>(null);
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [saving, setSaving] = useState(false);

  const getQuestForBoss = (bossName: string) => {
    return player.quests.find((q) => q.boss_name === bossName);
  };

  const handleSaveQuest = async (boss: string) => {
    const value = parseInt(editValue, 10);
    if (isNaN(value) || value < 0) return;

    setSaving(true);
    try {
      await onUpdateQuest(player.discord_id, player.player_name, boss, value);
      setEditingQuest(null);
      await onRefresh();
    } catch (error) {
      console.error('Failed to update quest:', error);
    } finally {
      setSaving(false);
    }
  };

  const handleSaveKey = async (keyType: string) => {
    const value = parseInt(editValue, 10);
    if (isNaN(value) || value < 0) return;

    setSaving(true);
    try {
      await onUpdateKeys(player.discord_id, player.player_name, keyType, value);
      setEditingKey(null);
      await onRefresh();
    } catch (error) {
      console.error('Failed to update keys:', error);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* Player Info */}
      <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-4">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h2 className="text-xl font-bold text-white flex items-center gap-2">
              {player.player_name}
              {player.is_alt && (
                <span className="text-sm px-2 py-0.5 bg-amber-900/50 text-amber-400 rounded">ALT</span>
              )}
            </h2>
            {player.is_alt ? (
              <p className="text-sm text-gray-400">Main character: {player.owner_name}</p>
            ) : (
              <p className="text-sm text-gray-400">Discord ID: {player.discord_id}</p>
            )}
          </div>
          {!player.is_alt && player.alts.length > 0 && (
            <div className="text-right">
              <div className="text-xs text-gray-500 mb-1">Alts</div>
              <div className="flex flex-wrap gap-1 justify-end">
                {player.alts.map((alt) => (
                  <span
                    key={alt}
                    className="px-2 py-0.5 text-xs bg-[var(--color-bg-input)] text-gray-300 rounded"
                  >
                    {alt}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
        
        {/* Admin Actions */}
        {!player.is_alt && (
          <div className="flex gap-2 pt-3 border-t border-[var(--color-border)]">
            <button
              onClick={onUnregister}
              className="px-3 py-1.5 text-xs text-amber-400 hover:text-amber-300 border border-amber-700 hover:border-amber-600 rounded transition-colors"
            >
              🔓 Unregister
            </button>
            <button
              onClick={onDelete}
              className="px-3 py-1.5 text-xs text-red-400 hover:text-red-300 border border-red-700 hover:border-red-600 rounded transition-colors"
            >
              🗑️ Delete All Data
            </button>
          </div>
        )}
      </div>

      {/* Quests */}
      <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
        <div className="px-4 py-3 border-b border-[var(--color-border)]">
          <h3 className="font-semibold text-white">Weekly Quests</h3>
        </div>
        <div className="divide-y divide-[var(--color-border)]">
          {BOSSES.map((boss) => {
            const quest = getQuestForBoss(boss.name);
            const remaining = quest ? quest.required_kills - quest.current_kills : 0;
            const isEditing = editingQuest === boss.name;

            return (
              <div
                key={boss.name}
                className="px-4 py-3 flex items-center justify-between"
              >
                <div className="flex items-center gap-3">
                  <div
                    className="w-3 h-3 rounded-full"
                    style={{ backgroundColor: boss.color }}
                  />
                  <span className="text-sm text-gray-200">{boss.label}</span>
                </div>
                <div className="flex items-center gap-2">
                  {isEditing ? (
                    <>
                      <input
                        type="number"
                        value={editValue}
                        onChange={(e) => setEditValue(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') handleSaveQuest(boss.name);
                          if (e.key === 'Escape') setEditingQuest(null);
                        }}
                        className="w-20 px-2 py-1 text-sm bg-[var(--color-bg-dark)] border border-red-500 rounded text-white text-center focus:outline-none"
                        min="0"
                        autoFocus
                        disabled={saving}
                      />
                      <button
                        onClick={() => handleSaveQuest(boss.name)}
                        disabled={saving}
                        className="px-2 py-1 text-xs bg-red-600 hover:bg-red-700 text-white rounded"
                      >
                        {saving ? '...' : 'Save'}
                      </button>
                      <button
                        onClick={() => setEditingQuest(null)}
                        className="px-2 py-1 text-xs bg-gray-600 hover:bg-gray-700 text-white rounded"
                      >
                        Cancel
                      </button>
                    </>
                  ) : (
                    <>
                      <span className={`text-sm font-mono ${remaining > 0 ? 'text-amber-400' : 'text-gray-500'}`}>
                        {remaining > 0 ? remaining : '—'}
                      </span>
                      <button
                        onClick={() => {
                          setEditingQuest(boss.name);
                          setEditValue((quest?.required_kills ?? 0).toString());
                        }}
                        className="px-2 py-1 text-xs text-gray-400 hover:text-red-400 transition-colors"
                      >
                        Edit
                      </button>
                    </>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Keys */}
      <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
        <div className="px-4 py-3 border-b border-[var(--color-border)]">
          <h3 className="font-semibold text-white">Boss Keys</h3>
        </div>
        <div className="divide-y divide-[var(--color-border)]">
          {KEY_TYPES.map((keyInfo) => {
            const count = player.keys[keyInfo.type] ?? 0;
            const isEditing = editingKey === keyInfo.type;

            return (
              <div
                key={keyInfo.type}
                className="px-4 py-3 flex items-center justify-between"
              >
                <div className="flex items-center gap-3">
                  <div
                    className="w-3 h-3 rounded-sm"
                    style={{ backgroundColor: keyInfo.color }}
                  />
                  <span className="text-sm text-gray-200">{keyInfo.label}</span>
                </div>
                <div className="flex items-center gap-2">
                  {isEditing ? (
                    <>
                      <input
                        type="number"
                        value={editValue}
                        onChange={(e) => setEditValue(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') handleSaveKey(keyInfo.type);
                          if (e.key === 'Escape') setEditingKey(null);
                        }}
                        className="w-20 px-2 py-1 text-sm bg-[var(--color-bg-dark)] border border-red-500 rounded text-white text-center focus:outline-none"
                        min="0"
                        autoFocus
                        disabled={saving}
                      />
                      <button
                        onClick={() => handleSaveKey(keyInfo.type)}
                        disabled={saving}
                        className="px-2 py-1 text-xs bg-red-600 hover:bg-red-700 text-white rounded"
                      >
                        {saving ? '...' : 'Save'}
                      </button>
                      <button
                        onClick={() => setEditingKey(null)}
                        className="px-2 py-1 text-xs bg-gray-600 hover:bg-gray-700 text-white rounded"
                      >
                        Cancel
                      </button>
                    </>
                  ) : (
                    <>
                      <span className={`text-sm font-mono ${count > 0 ? 'text-emerald-400' : 'text-gray-500'}`}>
                        {count}
                      </span>
                      <button
                        onClick={() => {
                          setEditingKey(keyInfo.type);
                          setEditValue(count.toString());
                        }}
                        className="px-2 py-1 text-xs text-gray-400 hover:text-red-400 transition-colors"
                      >
                        Edit
                      </button>
                    </>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

