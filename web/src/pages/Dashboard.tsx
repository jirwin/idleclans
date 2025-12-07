import { useEffect, useState, useCallback, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import type { UserData } from '../types';
import { KEY_TYPES, BOSSES } from '../types';
import { fetchUserData, updateQuest, updateKeys, logout } from '../api';
import { QuestCard } from '../components/QuestCard';
import { KeysCard } from '../components/KeysCard';
import { AltsManager } from '../components/AltsManager';
import { Registration } from '../components/Registration';
import { useSSE } from '../hooks/useSSE';

export function Dashboard() {
  const navigate = useNavigate();
  const [userData, setUserData] = useState<UserData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastRefresh, setLastRefresh] = useState<Date>(new Date());
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [selectedCharacter, setSelectedCharacter] = useState<string>('');
  const [showCharacterMenu, setShowCharacterMenu] = useState(false);

  const loadData = useCallback(async (showRefreshIndicator = false) => {
    if (showRefreshIndicator) {
      setIsRefreshing(true);
    }
    try {
      const data = await fetchUserData();
      setUserData(data);
      setError(null);
      setLastRefresh(new Date());
      
      // Set default selected character if not set
      if (!selectedCharacter && data.player_name) {
        setSelectedCharacter(data.player_name);
      }
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
  }, [navigate, selectedCharacter]);

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

  // Get all character names
  const allPlayerNames = userData ? [userData.player_name, ...userData.alts].filter(Boolean) : [];
  
  // Calculate totals for SELECTED character only
  const totals = useMemo(() => {
    if (!userData || !selectedCharacter) {
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

    // Only calculate for selected character
    const quests = userData.quests[selectedCharacter] || [];
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

    // Keys in inventory for selected character
    const playerKeys = userData.keys[selectedCharacter] || {};
    for (const [keyType, count] of Object.entries(playerKeys)) {
      keyTotals[keyType] = (keyTotals[keyType] || 0) + count;
    }

    const totalKeys = Object.values(keyTotals).reduce((sum, count) => sum + count, 0);
    const totalKeysRequired = Object.values(keysRequired).reduce((sum, count) => sum + count, 0);

    return { totalQuestsRemaining, totalBossesWithQuests, keyTotals, totalKeys, keysRequired, totalKeysRequired };
  }, [userData, selectedCharacter]);

  // Close menu when clicking outside
  useEffect(() => {
    const handleClickOutside = () => setShowCharacterMenu(false);
    if (showCharacterMenu) {
      document.addEventListener('click', handleClickOutside);
      return () => document.removeEventListener('click', handleClickOutside);
    }
  }, [showCharacterMenu]);

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

  // Show registration form if no character is linked
  if (!userData.player_name) {
    return (
      <Registration
        username={userData.username}
        avatar={userData.avatar}
        discordId={userData.discord_id}
        onSuccess={() => loadData()}
        onLogout={handleLogout}
      />
    );
  }

  const isAlt = selectedCharacter !== userData.player_name;

  return (
    <div className="min-h-screen p-4 md:p-8">
      <div className="max-w-2xl lg:max-w-5xl mx-auto">
        {/* Header */}
        <header className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-3">
            {userData.avatar && (
              <img
                src={`https://cdn.discordapp.com/avatars/${userData.discord_id}/${userData.avatar}.png`}
                alt=""
                className="w-10 h-10 rounded-full border-2 border-violet-500"
              />
            )}
            <div>
              <h1 className="text-lg font-bold text-white">{userData.username}</h1>
              <p className="text-xs text-gray-500">
                {lastRefresh.toLocaleTimeString()}
              </p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => navigate('/clan')}
              className="p-2 text-white bg-gradient-to-r from-violet-600 to-purple-600 hover:from-violet-500 hover:to-purple-500 rounded-lg transition-all"
              title="Clan View"
            >
              👥
            </button>
            <button
              onClick={handleManualRefresh}
              disabled={isRefreshing}
              className="p-2 text-gray-400 hover:text-violet-400 border border-gray-600 hover:border-violet-500 rounded-lg transition-colors disabled:opacity-50"
              title="Refresh"
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
              className="p-2 text-gray-400 hover:text-white border border-gray-600 hover:border-gray-500 rounded-lg transition-colors"
              title="Sign Out"
            >
              <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
              </svg>
            </button>
          </div>
        </header>

        {/* Character Switcher */}
        {allPlayerNames.length > 1 && (
          <div className="mb-4">
            <div className="relative">
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setShowCharacterMenu(!showCharacterMenu);
                }}
                className="w-full px-4 py-3 bg-[var(--color-bg-card)] border border-[var(--color-border)] rounded-xl flex items-center justify-between text-left hover:border-violet-500 transition-colors"
              >
                <div className="flex items-center gap-3">
                  <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-violet-600 to-purple-700 flex items-center justify-center text-sm">
                    {isAlt ? '👤' : '⭐'}
                  </div>
                  <div>
                    <div className="font-medium text-white">{selectedCharacter}</div>
                    <div className="text-xs text-gray-500">
                      {isAlt ? 'Alt Character' : 'Main Character'}
                    </div>
                  </div>
                </div>
                <svg
                  className={`w-5 h-5 text-gray-400 transition-transform ${showCharacterMenu ? 'rotate-180' : ''}`}
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                </svg>
              </button>
              
              {/* Dropdown Menu */}
              {showCharacterMenu && (
                <div className="absolute top-full left-0 right-0 mt-2 bg-[var(--color-bg-card)] border border-[var(--color-border)] rounded-xl overflow-hidden shadow-xl z-20">
                  {allPlayerNames.map((name) => {
                    const isMain = name === userData.player_name;
                    const isSelected = name === selectedCharacter;
                    return (
                      <button
                        key={name}
                        onClick={() => {
                          setSelectedCharacter(name);
                          setShowCharacterMenu(false);
                        }}
                        className={`w-full px-4 py-3 flex items-center gap-3 text-left transition-colors ${
                          isSelected
                            ? 'bg-violet-600/20 border-l-2 border-violet-500'
                            : 'hover:bg-[var(--color-bg-input)]'
                        }`}
                      >
                        <div className={`w-8 h-8 rounded-lg flex items-center justify-center text-sm ${
                          isMain 
                            ? 'bg-gradient-to-br from-amber-500 to-orange-600' 
                            : 'bg-gradient-to-br from-gray-600 to-gray-700'
                        }`}>
                          {isMain ? '⭐' : '👤'}
                        </div>
                        <div>
                          <div className={`font-medium ${isSelected ? 'text-violet-300' : 'text-white'}`}>
                            {name}
                          </div>
                          <div className="text-xs text-gray-500">
                            {isMain ? 'Main' : 'Alt'}
                          </div>
                        </div>
                        {isSelected && (
                          <svg className="w-5 h-5 text-violet-400 ml-auto" fill="currentColor" viewBox="0 0 20 20">
                            <path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" />
                          </svg>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
        )}

        {/* Single character display (no switcher needed) */}
        {allPlayerNames.length === 1 && (
          <div className="mb-4 px-4 py-3 bg-[var(--color-bg-card)] border border-[var(--color-border)] rounded-xl flex items-center gap-3">
            <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-amber-500 to-orange-600 flex items-center justify-center text-sm">
              ⭐
            </div>
            <div>
              <div className="font-medium text-white">{selectedCharacter}</div>
              <div className="text-xs text-gray-500">Main Character</div>
            </div>
          </div>
        )}

        {/* Stats Summary */}
        <div className="mb-6 bg-gradient-to-r from-violet-900/30 to-purple-900/30 rounded-xl border border-violet-700/50 p-4">
          <div className="grid grid-cols-4 gap-3">
            <div className="text-center">
              <div className="text-2xl font-bold text-amber-400">{totals.totalQuestsRemaining}</div>
              <div className="text-xs text-gray-400">Kills</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-bold text-violet-400">{totals.totalBossesWithQuests}</div>
              <div className="text-xs text-gray-400">Quests</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-bold text-red-400">{totals.totalKeysRequired}</div>
              <div className="text-xs text-gray-400">Need</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-bold text-emerald-400">{totals.totalKeys}</div>
              <div className="text-xs text-gray-400">Keys</div>
            </div>
          </div>

          {/* Key breakdown */}
          {(totals.totalKeysRequired > 0 || totals.totalKeys > 0) && (
            <div className="mt-3 pt-3 border-t border-violet-700/50">
              <div className="flex flex-wrap justify-center gap-2">
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
                        className="w-2 h-2 rounded-sm"
                        style={{ backgroundColor: keyInfo.color }}
                      />
                      <span className={`text-xs font-mono ${deficit > 0 ? 'text-red-400' : 'text-emerald-400'}`}>
                        {owned}/{required}
                      </span>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>

        {/* Quests and Keys for Selected Character */}
        <div className="grid gap-4 lg:grid-cols-2">
          <QuestCard
            playerName={selectedCharacter}
            quests={userData.quests[selectedCharacter] || []}
            onUpdateQuest={handleUpdateQuest}
          />
          
          <div className="space-y-4">
            <KeysCard
              playerName={selectedCharacter}
              keys={userData.keys[selectedCharacter] || {}}
              onUpdateKeys={handleUpdateKeys}
            />
            
            {/* Alts Manager */}
            <AltsManager
              alts={userData.alts}
              onUpdate={() => loadData()}
            />
          </div>
        </div>
      </div>
    </div>
  );
}
