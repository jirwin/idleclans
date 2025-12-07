import { useEffect, useState, useCallback, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import type { UserData } from '../types';
import { KEY_TYPES, BOSSES } from '../types';
import { fetchUserData, updateQuest, updateKeys, logout } from '../api';
import { QuestCard } from '../components/QuestCard';
import { KeysCard } from '../components/KeysCard';
import { useSSE } from '../hooks/useSSE';

export function Dashboard() {
  const navigate = useNavigate();
  const [userData, setUserData] = useState<UserData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastRefresh, setLastRefresh] = useState<Date>(new Date());
  const [isRefreshing, setIsRefreshing] = useState(false);

  const loadData = useCallback(async (showRefreshIndicator = false) => {
    if (showRefreshIndicator) {
      setIsRefreshing(true);
    }
    try {
      const data = await fetchUserData();
      setUserData(data);
      setError(null);
      setLastRefresh(new Date());
    } catch (err) {
      if (err instanceof Error && err.message === 'Unauthorized') {
        navigate('/');
        return;
      }
      setError('Failed to load data. Please try again.');
    } finally {
      setLoading(false);
      setIsRefreshing(false);
    }
  }, [navigate]);

  // Use SSE for live updates
  useSSE({
    onUpdate: loadData,
    enabled: !loading,
  });

  // Initial load
  useEffect(() => {
    loadData();
  }, [loadData]);

  // Refresh when window regains focus (fallback)
  useEffect(() => {
    const handleFocus = () => {
      loadData();
    };

    window.addEventListener('focus', handleFocus);
    return () => window.removeEventListener('focus', handleFocus);
  }, [loadData]);

  const handleManualRefresh = () => {
    loadData(true);
  };

  const handleUpdateQuest = async (playerName: string, boss: string, kills: number) => {
    await updateQuest(playerName, boss, kills);
    await loadData();
  };

  const handleUpdateKeys = async (playerName: string, keyType: string, count: number) => {
    await updateKeys(playerName, keyType, count);
    await loadData();
  };

  const handleLogout = async () => {
    await logout();
    navigate('/');
  };

  // Calculate totals across all characters (must be before early returns for hook rules)
  const allPlayerNames = userData ? [userData.player_name, ...userData.alts].filter(Boolean) : [];
  
  const totals = useMemo(() => {
    if (!userData) {
      return { 
        totalQuestsRemaining: 0, 
        totalBossesWithQuests: 0, 
        keyTotals: {}, 
        totalKeys: 0,
        keysRequired: {},
        totalKeysRequired: 0,
      };
    }

    let totalQuestsRemaining = 0;
    let totalBossesWithQuests = 0;
    const keyTotals: Record<string, number> = {};
    const keysRequired: Record<string, number> = {};

    // Sum up quests and calculate keys required
    for (const playerName of allPlayerNames) {
      const quests = userData.quests[playerName] || [];
      for (const quest of quests) {
        const remaining = quest.required_kills - quest.current_kills;
        if (remaining > 0) {
          totalQuestsRemaining += remaining;
          totalBossesWithQuests++;
          
          // Find the key type for this boss
          const bossInfo = BOSSES.find(b => b.name === quest.boss_name);
          if (bossInfo) {
            keysRequired[bossInfo.key] = (keysRequired[bossInfo.key] || 0) + remaining;
          }
        }
      }

      // Sum up keys in inventory
      const playerKeys = userData.keys[playerName] || {};
      for (const [keyType, count] of Object.entries(playerKeys)) {
        keyTotals[keyType] = (keyTotals[keyType] || 0) + count;
      }
    }

    const totalKeys = Object.values(keyTotals).reduce((sum, count) => sum + count, 0);
    const totalKeysRequired = Object.values(keysRequired).reduce((sum, count) => sum + count, 0);

    return { totalQuestsRemaining, totalBossesWithQuests, keyTotals, totalKeys, keysRequired, totalKeysRequired };
  }, [userData, allPlayerNames]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="flex flex-col items-center gap-4">
          <div className="w-12 h-12 border-4 border-violet-500 border-t-transparent rounded-full animate-spin" />
          <p className="text-gray-400">Loading...</p>
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

  if (!userData) {
    return null;
  }

  return (
    <div className="min-h-screen p-4 md:p-8">
      <div className="max-w-6xl mx-auto">
        {/* Header */}
        <header className="flex items-center justify-between mb-8">
          <div className="flex items-center gap-4">
            {userData.avatar && (
              <img
                src={`https://cdn.discordapp.com/avatars/${userData.discord_id}/${userData.avatar}.png`}
                alt=""
                className="w-12 h-12 rounded-full border-2 border-violet-500"
              />
            )}
            <div>
              <h1 className="text-xl font-bold text-white">{userData.username}</h1>
              <p className="text-sm text-gray-400">
                {userData.player_name || 'No character registered'}
              </p>
            </div>
          </div>
          <div className="flex items-center gap-3">
            <div className="text-right hidden sm:block">
              <p className="text-xs text-gray-500">
                Last updated: {lastRefresh.toLocaleTimeString()}
              </p>
            </div>
            <button
              onClick={handleManualRefresh}
              disabled={isRefreshing}
              className="p-2 text-gray-400 hover:text-violet-400 border border-gray-600 hover:border-violet-500 rounded-lg transition-colors disabled:opacity-50"
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
            <button
              onClick={handleLogout}
              className="px-4 py-2 text-sm text-gray-400 hover:text-white border border-gray-600 hover:border-gray-500 rounded-lg transition-colors"
            >
              Sign Out
            </button>
          </div>
        </header>

        {/* Summary Section */}
        {allPlayerNames.length > 0 && (
          <div className="mb-8 bg-gradient-to-r from-violet-900/30 to-purple-900/30 rounded-xl border border-violet-700/50 p-4">
            <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
              {/* Total Quests */}
              <div className="text-center">
                <div className="text-3xl font-bold text-amber-400">{totals.totalQuestsRemaining}</div>
                <div className="text-xs text-gray-400 mt-1">Kills Remaining</div>
              </div>
              
              {/* Active Bosses */}
              <div className="text-center">
                <div className="text-3xl font-bold text-violet-400">{totals.totalBossesWithQuests}</div>
                <div className="text-xs text-gray-400 mt-1">Active Quests</div>
              </div>
              
              {/* Keys Required */}
              <div className="text-center">
                <div className="text-3xl font-bold text-red-400">{totals.totalKeysRequired}</div>
                <div className="text-xs text-gray-400 mt-1">Keys Needed</div>
              </div>
              
              {/* Total Keys */}
              <div className="text-center">
                <div className="text-3xl font-bold text-emerald-400">{totals.totalKeys}</div>
                <div className="text-xs text-gray-400 mt-1">Keys Owned</div>
              </div>
              
              {/* Characters */}
              <div className="text-center">
                <div className="text-3xl font-bold text-blue-400">{allPlayerNames.length}</div>
                <div className="text-xs text-gray-400 mt-1">Characters</div>
              </div>
            </div>

            {/* Key breakdown - Required vs Owned */}
            {(totals.totalKeysRequired > 0 || totals.totalKeys > 0) && (
              <div className="mt-4 pt-4 border-t border-violet-700/50">
                <div className="flex flex-wrap justify-center gap-3">
                  {KEY_TYPES.map((keyInfo) => {
                    const owned = totals.keyTotals[keyInfo.type] || 0;
                    const required = totals.keysRequired[keyInfo.type] || 0;
                    if (owned === 0 && required === 0) return null;
                    const deficit = required - owned;
                    return (
                      <div
                        key={keyInfo.type}
                        className="flex items-center gap-1.5 px-2 py-1 bg-[var(--color-bg-card)] rounded-lg"
                        title={`${keyInfo.label}: ${owned} owned, ${required} needed`}
                      >
                        <div
                          className="w-2.5 h-2.5 rounded-sm"
                          style={{ backgroundColor: keyInfo.color }}
                        />
                        <span className={`text-sm ${deficit > 0 ? 'text-red-400' : 'text-emerald-400'}`}>
                          {owned}/{required}
                        </span>
                      </div>
                    );
                  })}
                </div>
                <div className="text-center text-xs text-gray-500 mt-2">
                  owned / needed
                </div>
              </div>
            )}
          </div>
        )}

        {/* No Character Message */}
        {!userData.player_name && (
          <div className="bg-amber-900/20 border border-amber-700/50 rounded-xl p-6 mb-8">
            <div className="flex items-start gap-4">
              <span className="text-2xl">⚠️</span>
              <div>
                <h2 className="font-semibold text-amber-400 mb-1">No Character Registered</h2>
                <p className="text-sm text-gray-300">
                  You haven't registered a character yet. Use the Discord bot command{' '}
                  <code className="bg-gray-800 px-2 py-0.5 rounded text-violet-300">!q register YourName</code>
                  {' '}to link your character to this Discord account.
                </p>
              </div>
            </div>
          </div>
        )}

        {/* Main Content */}
        <div className="grid gap-6 md:grid-cols-2">
          {/* Quests Section */}
          <div className="space-y-6">
            <h2 className="text-lg font-semibold text-white flex items-center gap-2">
              <span>⚔️</span> Weekly Quests
            </h2>
            {allPlayerNames.length > 0 ? (
              allPlayerNames.map((playerName) => (
                <QuestCard
                  key={playerName}
                  playerName={playerName}
                  quests={userData.quests[playerName] || []}
                  onUpdateQuest={handleUpdateQuest}
                />
              ))
            ) : (
              <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-8 text-center text-gray-500">
                No characters to display
              </div>
            )}
          </div>

          {/* Keys Section */}
          <div className="space-y-6">
            <h2 className="text-lg font-semibold text-white flex items-center gap-2">
              <span>🔑</span> Boss Keys
            </h2>
            {allPlayerNames.length > 0 ? (
              allPlayerNames.map((playerName) => (
                <KeysCard
                  key={playerName}
                  playerName={playerName}
                  keys={userData.keys[playerName] || {}}
                  onUpdateKeys={handleUpdateKeys}
                />
              ))
            ) : (
              <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-8 text-center text-gray-500">
                No characters to display
              </div>
            )}
            
            {/* Alts Info */}
            {userData.alts.length > 0 && (
              <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-4">
                <h3 className="text-sm font-semibold text-white mb-3 flex items-center gap-2">
                  <span>👥</span> Registered Alts
                </h3>
                <div className="flex flex-wrap gap-2">
                  {userData.alts.map((alt) => (
                    <span
                      key={alt}
                      className="px-3 py-1 text-sm bg-[var(--color-bg-input)] text-gray-300 rounded-full"
                    >
                      {alt}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {/* Quick Info */}
            <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-4">
              <h3 className="text-sm font-semibold text-white mb-3 flex items-center gap-2">
                <span>💡</span> Quick Tips
              </h3>
              <ul className="text-sm text-gray-400 space-y-2">
                <li>• Click the edit icon to update quest kills remaining</li>
                <li>• Press Enter to save, Escape to cancel</li>
                <li>• Each character has their own key counts</li>
                <li>• Quests reset every Monday at midnight UTC</li>
              </ul>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}


