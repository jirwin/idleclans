import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import type { ClanBossData, ClanKeysData, PlanData } from '../types';
import { BOSSES, KEY_TYPES } from '../types';
import { fetchClanBosses, fetchClanKeys, fetchClanPlan, fetchClanPlayers, sendPlanToDiscord } from '../api';
import { useSSE } from '../hooks/useSSE';

type TabType = 'bosses' | 'keys' | 'plan';
const MAX_PARTY_SIZE = 3;
const MIN_PARTY_SIZE = 1;

export function Clan() {
  const navigate = useNavigate();
  const [activeTab, setActiveTab] = useState<TabType>('bosses');
  const [bossData, setBossData] = useState<ClanBossData | null>(null);
  const [keysData, setKeysData] = useState<ClanKeysData | null>(null);
  const [planData, setPlanData] = useState<PlanData | null>(null);
  const [allPlayers, setAllPlayers] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [planLoading, setPlanLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastRefresh, setLastRefresh] = useState<Date>(new Date());
  const [showSendConfirm, setShowSendConfirm] = useState(false);
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState<string | null>(null);
  const [sent, setSent] = useState(false);
  
  // Selected players for plan (up to 3)
  const [selectedPlayers, setSelectedPlayers] = useState<Set<string>>(new Set());

  const loadData = useCallback(async () => {
    try {
      const [bosses, keys, players] = await Promise.all([
        fetchClanBosses(),
        fetchClanKeys(),
        fetchClanPlayers(),
      ]);
      setBossData(bosses);
      setKeysData(keys);
      setAllPlayers(players);
      setError(null);
      setLastRefresh(new Date());
    } catch (err) {
      if (err instanceof Error && err.message === 'Unauthorized') {
        navigate('/');
        return;
      }
      setError('Failed to load clan data');
    } finally {
      setLoading(false);
    }
  }, [navigate]);

  const loadPlan = useCallback(async (onlinePlayers?: string[]) => {
    setPlanLoading(true);
    try {
      const plan = await fetchClanPlan(onlinePlayers);
      setPlanData(plan);
    } catch (err) {
      console.error('Failed to load plan:', err);
    } finally {
      setPlanLoading(false);
    }
  }, []);

  // Use SSE for live updates
  useSSE({
    onUpdate: loadData,
    enabled: !loading,
  });

  useEffect(() => {
    loadData();
  }, [loadData]);

  // Auto-generate plan when players change
  useEffect(() => {
    if (activeTab === 'plan' && selectedPlayers.size >= MIN_PARTY_SIZE && selectedPlayers.size <= MAX_PARTY_SIZE) {
      loadPlan(Array.from(selectedPlayers));
    } else if (activeTab === 'plan' && selectedPlayers.size === 0) {
      setPlanData(null);
    }
  }, [selectedPlayers, activeTab, loadPlan]);

  const togglePlayer = (name: string) => {
    setSelectedPlayers(prev => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else if (next.size < MAX_PARTY_SIZE) {
        next.add(name);
      }
      return next;
    });
  };

  const clearSelection = () => {
    setSelectedPlayers(new Set());
    setPlanData(null);
  };

  const handleSendToDiscord = async () => {
    if (selectedPlayers.size === 0) return;
    setSending(true);
    setSendError(null);
    try {
      await sendPlanToDiscord(Array.from(selectedPlayers));
      setSent(true);
      setShowSendConfirm(false);
      setTimeout(() => setSent(false), 3000);
    } catch (err) {
      setSendError(err instanceof Error ? err.message : 'Failed to send');
    } finally {
      setSending(false);
    }
  };

  // Get boss info
  const getBossInfo = (bossName: string) => {
    return BOSSES.find(b => b.name === bossName);
  };

  // Sort entries by count descending, then by name
  const sortEntries = <T extends { player_name: string }>(
    entries: T[],
    getCount: (e: T) => number
  ) => {
    return [...entries].sort((a, b) => {
      const countDiff = getCount(b) - getCount(a);
      if (countDiff !== 0) return countDiff;
      return a.player_name.localeCompare(b.player_name);
    });
  };

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="w-12 h-12 border-4 border-violet-500 border-t-transparent rounded-full animate-spin mx-auto mb-4" />
          <p className="text-gray-400">Loading clan data...</p>
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
            onClick={() => loadData()}
            className="px-4 py-2 bg-violet-600 hover:bg-violet-700 text-white rounded-lg transition-colors"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen p-4 md:p-8">
      <div className="max-w-4xl mx-auto">
        {/* Header */}
        <header className="flex items-center justify-between mb-6">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-gradient-to-br from-violet-600 to-purple-700 flex items-center justify-center">
              <span className="text-xl">👥</span>
            </div>
            <div>
              <h1 className="text-xl font-bold text-white">Clan Overview</h1>
              <p className="text-xs text-gray-500">
                Updated: {lastRefresh.toLocaleTimeString()}
              </p>
            </div>
          </div>
          <button
            onClick={() => navigate('/dashboard')}
            className="px-4 py-2 text-sm text-gray-400 hover:text-white border border-gray-600 hover:border-gray-500 rounded-lg transition-colors"
          >
            My Quests
          </button>
        </header>

        {/* Tabs */}
        <div className="flex gap-2 mb-6">
          <button
            onClick={() => setActiveTab('bosses')}
            className={`flex-1 px-4 py-3 text-sm font-medium rounded-lg transition-colors ${
              activeTab === 'bosses'
                ? 'bg-violet-600 text-white'
                : 'bg-[var(--color-bg-card)] text-gray-400 hover:text-white border border-[var(--color-border)]'
            }`}
          >
            <span className="mr-2">⚔️</span>
            <span className="hidden sm:inline">Who Has </span>Bosses
          </button>
          <button
            onClick={() => setActiveTab('keys')}
            className={`flex-1 px-4 py-3 text-sm font-medium rounded-lg transition-colors ${
              activeTab === 'keys'
                ? 'bg-amber-600 text-white'
                : 'bg-[var(--color-bg-card)] text-gray-400 hover:text-white border border-[var(--color-border)]'
            }`}
          >
            <span className="mr-2">🔑</span>
            <span className="hidden sm:inline">Who Has </span>Keys
          </button>
          <button
            onClick={() => setActiveTab('plan')}
            className={`flex-1 px-4 py-3 text-sm font-medium rounded-lg transition-colors ${
              activeTab === 'plan'
                ? 'bg-pink-600 text-white'
                : 'bg-[var(--color-bg-card)] text-gray-400 hover:text-white border border-[var(--color-border)]'
            }`}
          >
            <span className="mr-2">📋</span>
            <span className="hidden sm:inline">Party </span>Plan
          </button>
        </div>

        {/* Bosses Tab */}
        {activeTab === 'bosses' && bossData && (
          <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
            <div className="px-4 py-3 border-b border-[var(--color-border)]">
              <h2 className="font-semibold text-white flex items-center gap-2">
                <span>⚔️</span>
                Who Has Bosses
              </h2>
              <p className="text-xs text-gray-500 mt-1">
                Week {bossData.week}, {bossData.year}
              </p>
            </div>

            {Object.keys(bossData.bosses).length === 0 ? (
              <div className="p-8 text-center text-gray-500">
                <p>No active boss quests this week</p>
                <p className="text-sm mt-1">All quests have been completed! 🎉</p>
              </div>
            ) : (
              <div className="grid gap-4 p-4 sm:grid-cols-2 lg:grid-cols-3">
                {BOSSES.map((boss) => {
                  const entries = bossData.bosses[boss.name];
                  if (!entries || entries.length === 0) return null;

                  const sortedEntries = sortEntries(entries, (e) => e.remaining_kills);
                  const totalKills = sortedEntries.reduce((sum, e) => sum + e.remaining_kills, 0);

                  return (
                    <div
                      key={boss.name}
                      className="bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)] overflow-hidden"
                    >
                      <div
                        className="px-3 py-2 flex items-center justify-between"
                        style={{ borderBottom: `2px solid ${boss.color}` }}
                      >
                        <div className="flex items-center gap-2">
                          <div
                            className="w-3 h-3 rounded-full"
                            style={{ backgroundColor: boss.color }}
                          />
                          <span className="font-medium text-white text-sm">{boss.label}</span>
                        </div>
                        <span className="text-xs text-gray-400">{totalKills} total</span>
                      </div>
                      <div className="p-2 space-y-1 max-h-40 overflow-y-auto">
                        {sortedEntries.map((entry) => (
                          <div
                            key={entry.player_name}
                            className="flex items-center justify-between px-2 py-1 rounded hover:bg-[var(--color-bg-input)]"
                          >
                            <span className="text-sm text-gray-300 truncate">
                              {entry.player_name}
                            </span>
                            <span className="text-sm font-mono text-amber-400 ml-2">
                              {entry.remaining_kills}
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}

        {/* Keys Tab */}
        {activeTab === 'keys' && keysData && (
          <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
            <div className="px-4 py-3 border-b border-[var(--color-border)]">
              <h2 className="font-semibold text-white flex items-center gap-2">
                <span>🔑</span>
                Who Has Keys
              </h2>
              <p className="text-xs text-gray-500 mt-1">All registered players</p>
            </div>

            {Object.keys(keysData.keys).length === 0 ? (
              <div className="p-8 text-center text-gray-500">
                <p>No keys tracked yet</p>
              </div>
            ) : (
              <div className="grid gap-4 p-4 sm:grid-cols-2 lg:grid-cols-3">
                {KEY_TYPES.map((keyInfo) => {
                  const entries = keysData.keys[keyInfo.type];
                  if (!entries || entries.length === 0) return null;

                  const sortedEntries = sortEntries(entries, (e) => e.count);
                  const totalKeys = sortedEntries.reduce((sum, e) => sum + e.count, 0);

                  return (
                    <div
                      key={keyInfo.type}
                      className="bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)] overflow-hidden"
                    >
                      <div
                        className="px-3 py-2 flex items-center justify-between"
                        style={{ borderBottom: `2px solid ${keyInfo.color}` }}
                      >
                        <div className="flex items-center gap-2">
                          <div
                            className="w-3 h-3 rounded-sm"
                            style={{ backgroundColor: keyInfo.color }}
                          />
                          <span className="font-medium text-white text-sm">{keyInfo.label}</span>
                        </div>
                        <span className="text-xs text-gray-400">{totalKeys} total</span>
                      </div>
                      <div className="p-2 space-y-1 max-h-40 overflow-y-auto">
                        {sortedEntries.map((entry) => (
                          <div
                            key={entry.player_name}
                            className="flex items-center justify-between px-2 py-1 rounded hover:bg-[var(--color-bg-input)]"
                          >
                            <span className="text-sm text-gray-300 truncate">
                              {entry.player_name}
                            </span>
                            <span className="text-sm font-mono text-emerald-400 ml-2">
                              {entry.count}
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}

        {/* Plan Tab */}
        {activeTab === 'plan' && (
          <div className="space-y-4">
            {/* Player Selection */}
            <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
              <div className="px-4 py-3 border-b border-[var(--color-border)] flex items-center justify-between flex-wrap gap-2">
                <div>
                  <h3 className="font-semibold text-white text-sm">Select Party Members</h3>
                  <p className="text-xs text-gray-500">
                    {selectedPlayers.size === 0 
                      ? `Pick 1-${MAX_PARTY_SIZE} players to form a party` 
                      : `${selectedPlayers.size}/${MAX_PARTY_SIZE} selected`}
                  </p>
                </div>
                {selectedPlayers.size > 0 && (
                  <button
                    onClick={clearSelection}
                    className="px-3 py-1.5 text-xs text-gray-400 hover:text-white border border-gray-600 rounded transition-colors"
                  >
                    Clear
                  </button>
                )}
              </div>
              
              {/* Selected players */}
              {selectedPlayers.size > 0 && (
                <div className="px-4 py-2 bg-pink-900/20 border-b border-[var(--color-border)]">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-xs text-gray-400">Party:</span>
                    {Array.from(selectedPlayers).map((name) => (
                      <span
                        key={name}
                        className="px-2 py-0.5 text-xs bg-pink-600 text-white rounded-full flex items-center gap-1"
                      >
                        {name}
                        <button
                          onClick={() => togglePlayer(name)}
                          className="hover:text-pink-200"
                        >
                          ×
                        </button>
                      </span>
                    ))}
                  </div>
                </div>
              )}
              
              {/* Player list */}
              <div className="p-3 flex flex-wrap gap-2 max-h-48 overflow-y-auto">
                {allPlayers.map((name) => {
                  const isSelected = selectedPlayers.has(name);
                  const isDisabled = !isSelected && selectedPlayers.size >= MAX_PARTY_SIZE;
                  return (
                    <button
                      key={name}
                      onClick={() => togglePlayer(name)}
                      disabled={isDisabled}
                      className={`px-3 py-1.5 text-xs rounded-lg transition-colors ${
                        isSelected
                          ? 'bg-pink-600 text-white'
                          : isDisabled
                          ? 'bg-[var(--color-bg-input)] text-gray-600 cursor-not-allowed border border-[var(--color-border)]'
                          : 'bg-[var(--color-bg-input)] text-gray-400 hover:text-white border border-[var(--color-border)] hover:border-pink-500'
                      }`}
                    >
                      {name}
                    </button>
                  );
                })}
                {allPlayers.length === 0 && (
                  <p className="text-sm text-gray-500">No registered players</p>
                )}
              </div>
            </div>

            {/* Plan Results */}
            {planLoading ? (
              <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-8 text-center">
                <div className="w-8 h-8 border-4 border-pink-500 border-t-transparent rounded-full animate-spin mx-auto mb-4" />
                <p className="text-gray-400">Generating party plan...</p>
              </div>
            ) : planData ? (
              <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
                <div className="px-4 py-3 border-b border-[var(--color-border)] flex items-center justify-between flex-wrap gap-2">
                  <div>
                    <h2 className="font-semibold text-white flex items-center gap-2">
                      <span>📋</span>
                      Party Plan
                    </h2>
                    <p className="text-xs text-gray-500 mt-1">
                      Week {planData.week}, {planData.year} • Groups to minimize party switching
                    </p>
                  </div>
                  <button
                    onClick={() => setShowSendConfirm(true)}
                    disabled={sending}
                    className={`px-4 py-2 text-sm font-medium rounded-lg transition-colors flex items-center gap-2 ${
                      sent
                        ? 'bg-emerald-600 text-white'
                        : 'bg-indigo-600 hover:bg-indigo-700 text-white'
                    }`}
                  >
                    {sent ? (
                      <>✓ Sent!</>
                    ) : sending ? (
                      <>Sending...</>
                    ) : (
                      <>📤 Send to Discord</>
                    )}
                  </button>
                </div>

                {planData.parties.length === 0 && planData.leftovers.length === 0 ? (
                  <div className="p-8 text-center text-gray-500">
                    <p>No quests to plan for</p>
                    <p className="text-sm mt-1">
                      {selectedPlayers.size > 0 
                        ? 'Selected players have no active quests' 
                        : 'All quests have been completed! 🎉'}
                    </p>
                  </div>
                ) : (
                  <div className="p-4 space-y-4">
                    {/* Parties */}
                    {planData.parties.map((party, index) => (
                      <div
                        key={index}
                        className="bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)] overflow-hidden"
                      >
                        <div className="px-4 py-2 bg-pink-900/20 border-b border-[var(--color-border)]">
                          <span className="font-medium text-white">
                            Group {index + 1}: {party.players.join(', ')}
                          </span>
                        </div>
                        <div className="p-3 space-y-2">
                          {party.tasks.map((task, taskIndex) => {
                            const bossInfo = getBossInfo(task.boss_name);
                            return (
                              <div
                                key={taskIndex}
                                className="flex items-center justify-between text-sm"
                              >
                                <div className="flex items-center gap-2">
                                  <div
                                    className="w-2.5 h-2.5 rounded-full"
                                    style={{ backgroundColor: bossInfo?.color || '#888' }}
                                  />
                                  <span className="text-gray-200">
                                    {bossInfo?.label || task.boss_name}
                                  </span>
                                  <span className="text-amber-400 font-mono">{task.kills}</span>
                                </div>
                                {task.no_keys ? (
                                  <span className="text-xs text-red-400 flex items-center gap-1">
                                    ⚠️ No keys
                                  </span>
                                ) : (
                                  <span className="text-xs text-gray-500">
                                    Key: {task.key_holder}
                                  </span>
                                )}
                              </div>
                            );
                          })}
                        </div>
                      </div>
                    ))}

                    {/* Leftovers */}
                    {planData.leftovers.length > 0 && (
                      <div className="bg-[var(--color-bg-dark)] rounded-lg border border-amber-700/50 overflow-hidden">
                        <div className="px-4 py-2 bg-amber-900/20 border-b border-amber-700/50">
                          <span className="font-medium text-amber-400">
                            ⚠️ Unmatched / Leftovers
                          </span>
                        </div>
                        <div className="p-3 space-y-2">
                          {planData.leftovers.slice(0, 10).map((leftover) => (
                            <div key={leftover.player_name} className="text-sm">
                              <span className="font-medium text-gray-200">{leftover.player_name}:</span>
                              <span className="text-gray-400 ml-2">
                                {Object.entries(leftover.needs)
                                  .map(([boss, count]) => {
                                    const bossInfo = getBossInfo(boss);
                                    return `${bossInfo?.label || boss} (${count})`;
                                  })
                                  .join(', ')}
                              </span>
                            </div>
                          ))}
                          {planData.leftovers.length > 10 && (
                            <p className="text-xs text-gray-500">
                              ...and {planData.leftovers.length - 10} more
                            </p>
                          )}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            ) : (
              <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-8 text-center">
                <div className="text-4xl mb-3">👥</div>
                <p className="text-gray-400">Select 1-{MAX_PARTY_SIZE} players above to generate a plan</p>
                <p className="text-xs text-gray-500 mt-2">
                  The plan will show which bosses to kill together and who provides keys
                </p>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Send to Discord Confirmation Modal */}
      {showSendConfirm && (
        <div className="fixed inset-0 bg-black/70 flex items-center justify-center p-4 z-50">
          <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] max-w-sm w-full p-6">
            <h3 className="text-lg font-semibold text-white mb-2 flex items-center gap-2">
              <span>📤</span>
              Send Plan to Discord
            </h3>
            <p className="text-gray-400 text-sm mb-4">
              This will post the party plan to Discord and ping the selected players.
            </p>

            {sendError && (
              <div className="bg-red-900/30 border border-red-700 rounded-lg p-3 mb-4">
                <p className="text-sm text-red-400">{sendError}</p>
              </div>
            )}

            <div className="flex gap-3 justify-end">
              <button
                onClick={() => {
                  setShowSendConfirm(false);
                  setSendError(null);
                }}
                className="px-4 py-2 text-sm text-gray-400 hover:text-white border border-gray-600 rounded-lg transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleSendToDiscord}
                disabled={sending}
                className="px-4 py-2 text-sm font-medium bg-indigo-600 hover:bg-indigo-700 disabled:bg-gray-600 text-white rounded-lg transition-colors flex items-center gap-2"
              >
                {sending ? (
                  <>
                    <div className="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin" />
                    Sending...
                  </>
                ) : (
                  <>📤 Send & Ping</>
                )}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
