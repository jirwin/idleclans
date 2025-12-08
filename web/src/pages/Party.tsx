import { useEffect, useState, useCallback, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import type { PartySession, PlanPartyTask, UserData } from '../types';
import { BOSSES } from '../types';
import {
  getParty,
  startPartyStep,
  updatePartyKills,
  updatePartyKeys,
  nextPartyStep,
  endParty,
  fetchUserData,
} from '../api';
import { useSSE } from '../hooks/useSSE';

// Helper to get boss info
function getBossInfo(bossName: string) {
  return BOSSES.find((b) => b.name === bossName);
}

// Helper to format time duration
function formatDuration(seconds: number): string {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const secs = seconds % 60;
  if (hours > 0) {
    return `${hours}:${minutes.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
  }
  return `${minutes}:${secs.toString().padStart(2, '0')}`;
}

// Flatten all tasks from plan
function getAllTasks(party: PartySession): PlanPartyTask[] {
  const tasks: PlanPartyTask[] = [];
  for (const p of party.plan.parties) {
    tasks.push(...p.tasks);
  }
  return tasks;
}

export function Party() {
  const { partyId } = useParams<{ partyId: string }>();
  const navigate = useNavigate();

  const [party, setParty] = useState<PartySession | null>(null);
  const [userData, setUserData] = useState<UserData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedPlayer, setSelectedPlayer] = useState<string | null>(null);
  const [killInput, setKillInput] = useState<string>('');
  const [keysUsedInput, setKeysUsedInput] = useState<string>('');
  const [showEndConfirm, setShowEndConfirm] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);
  const [conflictError, setConflictError] = useState<string | null>(null);

  // Timer state
  const [elapsedSeconds, setElapsedSeconds] = useState(0);
  const timerRef = useRef<number | null>(null);

  // Track if we have a pending kill update to avoid flicker
  const pendingKillUpdateRef = useRef<boolean>(false);
  // Track if we're currently fetching to prevent concurrent loads
  const isFetchingRef = useRef<boolean>(false);

  const loadParty = useCallback(async () => {
    if (!partyId) return;
    
    // Prevent concurrent fetches
    if (isFetchingRef.current) {
      return;
    }
    isFetchingRef.current = true;
    
    try {
      const data = await getParty(partyId);
      setParty(data);
      setError(null);

      // Set selected player to first player if not set
      setSelectedPlayer(prev => {
        if (!prev && data.players.length > 0) {
          return data.players[0];
        }
        return prev;
      });

      // Update kills input to current value (skip if we have a pending update to avoid flicker)
      const allTasks = getAllTasks(data);
      if (data.current_step_index < allTasks.length) {
        const progress = data.step_progress.find(
          (p) => p.step_index === data.current_step_index
        );
        
        if (!pendingKillUpdateRef.current) {
          setKillInput(progress ? progress.kills_tracked.toString() : '0');
        }
        setKeysUsedInput(progress ? progress.keys_used.toString() : '0');
      }
    } catch (err) {
      if (err instanceof Error) {
        if (err.message === 'Unauthorized') {
          navigate('/');
          return;
        }
        setError(err.message);
      } else {
        setError('Failed to load party');
      }
    } finally {
      setLoading(false);
      isFetchingRef.current = false;
    }
  }, [partyId, navigate]);

  const loadUserData = useCallback(async () => {
    try {
      const data = await fetchUserData();
      setUserData(data);
    } catch {
      // Ignore errors - user data is optional for display
    }
  }, []);

  // SSE for live updates
  useSSE({
    onUpdate: loadParty,
    enabled: !loading && !!party,
  });

  useEffect(() => {
    loadParty();
    loadUserData();
  }, [loadParty, loadUserData]);

  // Timer effect
  useEffect(() => {
    if (!party?.started_at || party.ended_at) {
      if (timerRef.current) {
        clearInterval(timerRef.current);
        timerRef.current = null;
      }
      return;
    }

    const startTime = new Date(party.started_at).getTime();

    const updateTimer = () => {
      const now = Date.now();
      setElapsedSeconds(Math.floor((now - startTime) / 1000));
    };

    updateTimer();
    timerRef.current = window.setInterval(updateTimer, 1000);

    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current);
      }
    };
  }, [party?.started_at, party?.ended_at]);

  // Check if user owns a player
  const userOwnsPlayer = useCallback(
    (playerName: string): boolean => {
      if (!userData) return false;
      if (userData.player_name === playerName) return true;
      return userData.alts.includes(playerName);
    },
    [userData]
  );

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="w-12 h-12 border-4 border-violet-500 border-t-transparent rounded-full animate-spin mx-auto mb-4" />
          <p className="text-gray-400">Loading party...</p>
        </div>
      </div>
    );
  }

  if (error || !party) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <p className="text-red-400 mb-4">{error || 'Party not found'}</p>
          <button
            onClick={() => navigate('/clan')}
            className="px-4 py-2 bg-violet-600 hover:bg-violet-700 text-white rounded-lg transition-colors"
          >
            Back to Clan
          </button>
        </div>
      </div>
    );
  }

  const allTasks = getAllTasks(party);
  const currentTask = allTasks[party.current_step_index];
  const nextTask =
    party.current_step_index + 1 < allTasks.length
      ? allTasks[party.current_step_index + 1]
      : null;

  const currentProgress = party.step_progress.find(
    (p) => p.step_index === party.current_step_index
  );

  const isEnded = !!party.ended_at;
  const isStarted = !!party.started_at;

  // Check if current user is the leader
  const isCurrentLeader = currentTask?.key_holder
    ? userOwnsPlayer(currentTask.key_holder)
    : false;

  // Check if leader is changing for next step
  const leaderChanging =
    nextTask &&
    currentTask &&
    nextTask.key_holder !== currentTask.key_holder &&
    nextTask.key_holder !== '';

  // Calculate max kills needed for current boss (from plan)
  const maxKillsNeeded = currentTask?.kills || 0;

  // Handler functions
  const handleStartStep = async () => {
    if (!partyId || isEnded) return;
    setActionLoading(true);
    try {
      await startPartyStep(partyId);
      await loadParty();
    } catch (err) {
      console.error('Failed to start step:', err);
    } finally {
      setActionLoading(false);
    }
  };

  // Helper to update party progress optimistically
  const updatePartyProgressOptimistically = (newKills: number) => {
    if (!party) return;
    
    setParty(prev => {
      if (!prev) return prev;
      const newProgress = [...prev.step_progress];
      const idx = newProgress.findIndex(p => p.step_index === prev.current_step_index);
      
      if (idx >= 0) {
        // Update existing entry
        newProgress[idx] = { ...newProgress[idx], kills_tracked: newKills };
      } else {
        // Create new entry if doesn't exist
        const allTasks = getAllTasks(prev);
        const currentTask = allTasks[prev.current_step_index];
        if (currentTask) {
          newProgress.push({
            step_index: prev.current_step_index,
            boss_name: currentTask.boss_name,
            kills_tracked: newKills,
            keys_used: 0,
            started_at: new Date().toISOString(),
            completed_at: null,
          });
        }
      }
      return { ...prev, step_progress: newProgress };
    });
  };

  const handleKillsChange = async (newKills: number) => {
    if (!partyId || isEnded) return;
    if (newKills < 0) newKills = 0;

    // Clear any previous conflict error
    setConflictError(null);

    // Get current server value for optimistic concurrency
    const expectedKills = currentProgress?.kills_tracked ?? 0;

    // Mark that we have a pending update
    pendingKillUpdateRef.current = true;

    // Optimistically update immediately
    setKillInput(newKills.toString());
    updatePartyProgressOptimistically(newKills);

    try {
      const result = await updatePartyKills(partyId, newKills, false, expectedKills);
      
      if (result.conflict) {
        // Conflict detected - show error and update to actual value
        setConflictError('Update failed - another user changed the count. Please try again.');
        setKillInput(result.actual_kills!.toString());
        updatePartyProgressOptimistically(result.actual_kills!);
        // Auto-clear error after 3 seconds
        setTimeout(() => setConflictError(null), 3000);
      } else {
        // Update with server's confirmed value
        setKillInput(result.kills.toString());
        updatePartyProgressOptimistically(result.kills);
      }
      // Refresh full party data to update player_quests (small delay to ensure DB commit)
      setTimeout(() => loadParty(), 100);
    } catch (err) {
      console.error('Failed to update kills:', err);
      setConflictError(err instanceof Error ? err.message : 'Failed to update kills. Please try again.');
      setTimeout(() => setConflictError(null), 5000);
      loadParty();
    } finally {
      pendingKillUpdateRef.current = false;
    }
  };

  const handleKillsDelta = async (delta: number) => {
    if (!partyId || isEnded) return;
    const currentKills = parseInt(killInput) || 0;
    const optimisticKills = Math.max(0, currentKills + delta);

    // Clear any previous conflict error
    setConflictError(null);

    // Get current server value for optimistic concurrency
    const expectedKills = currentProgress?.kills_tracked ?? 0;

    // Mark that we have a pending update
    pendingKillUpdateRef.current = true;

    // Optimistically update immediately
    setKillInput(optimisticKills.toString());
    updatePartyProgressOptimistically(optimisticKills);

    try {
      const result = await updatePartyKills(partyId, delta, true, expectedKills);
      
      if (result.conflict) {
        // Conflict detected - show error and update to actual value
        setConflictError('Update failed - another user changed the count. Please try again.');
        setKillInput(result.actual_kills!.toString());
        updatePartyProgressOptimistically(result.actual_kills!);
        // Auto-clear error after 3 seconds
        setTimeout(() => setConflictError(null), 3000);
      } else {
        // Update with server's confirmed value
        setKillInput(result.kills.toString());
        updatePartyProgressOptimistically(result.kills);
      }
      // Refresh full party data to update player_quests (small delay to ensure DB commit)
      setTimeout(() => loadParty(), 100);
    } catch (err) {
      console.error('Failed to update kills:', err);
      setConflictError(err instanceof Error ? err.message : 'Failed to update kills. Please try again.');
      setTimeout(() => setConflictError(null), 5000);
      loadParty();
    } finally {
      pendingKillUpdateRef.current = false;
    }
  };

  const handleKeysChange = async () => {
    if (!partyId || isEnded || !isCurrentLeader) return;
    const keysUsed = parseInt(keysUsedInput) || 0;

    setActionLoading(true);
    try {
      await updatePartyKeys(partyId, keysUsed);
      await loadParty();
    } catch (err) {
      console.error('Failed to update keys:', err);
    } finally {
      setActionLoading(false);
    }
  };

  const handleNextStep = async () => {
    if (!partyId || isEnded) return;
    setActionLoading(true);
    try {
      await nextPartyStep(partyId);
      await loadParty();
    } catch (err) {
      console.error('Failed to advance step:', err);
    } finally {
      setActionLoading(false);
    }
  };

  const handleEndParty = async () => {
    if (!partyId || isEnded) return;
    setActionLoading(true);
    try {
      await endParty(partyId);
      setShowEndConfirm(false);
      await loadParty();
    } catch (err) {
      console.error('Failed to end party:', err);
    } finally {
      setActionLoading(false);
    }
  };

  // Get player quest progress for selected player
  const getPlayerQuestProgress = () => {
    if (!selectedPlayer || !party) return [];
    const quests = party.player_quests[selectedPlayer];
    if (!quests) return [];
    return quests.filter((q) => q.required_kills > 0);
  };

  const bossInfo = currentTask ? getBossInfo(currentTask.boss_name) : null;
  const nextBossInfo = nextTask ? getBossInfo(nextTask.boss_name) : null;

  return (
    <div className="min-h-screen p-4 md:p-8">
      <div className="max-w-4xl mx-auto space-y-4">
        {/* Header with Timer */}
        <header className="flex items-center justify-between flex-wrap gap-4">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-gradient-to-br from-pink-600 to-rose-700 flex items-center justify-center">
              <span className="text-xl">⚔</span>
            </div>
            <div>
              <h1 className="text-xl font-bold text-white">Party Session</h1>
              <p className="text-xs text-gray-500">
                {isEnded ? 'Ended' : isStarted ? 'In Progress' : 'Not Started'}
              </p>
            </div>
          </div>

          {/* Timer */}
          {isStarted && (
            <div className="text-center">
              <div className="text-3xl font-mono font-bold text-white">
                {formatDuration(elapsedSeconds)}
              </div>
              <div className="text-xs text-gray-500">Elapsed Time</div>
            </div>
          )}

          <button
            onClick={() => navigate('/clan')}
            className="px-4 py-2 text-sm text-gray-400 hover:text-white border border-gray-600 hover:border-gray-500 rounded-lg transition-colors"
          >
            Back to Clan
          </button>
        </header>

        {/* Player Stats Selector */}
        <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-4">
          <div className="flex items-center justify-between flex-wrap gap-4">
            <div>
              <h3 className="font-semibold text-white text-sm mb-1">
                Player Quest Progress
              </h3>
              <select
                value={selectedPlayer || ''}
                onChange={(e) => setSelectedPlayer(e.target.value)}
                className="bg-[var(--color-bg-input)] border border-[var(--color-border)] rounded-lg px-3 py-2 text-sm text-white"
              >
                {party.players.map((name) => (
                  <option key={name} value={name}>
                    {name}
                    {userOwnsPlayer(name) ? ' (You)' : ''}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-wrap gap-2">
              {getPlayerQuestProgress().map((quest) => {
                const info = getBossInfo(quest.boss_name);
                const remaining = quest.required_kills - quest.current_kills;
                return (
                  <div
                    key={quest.boss_name}
                    className="flex items-center gap-2 px-3 py-1.5 bg-[var(--color-bg-dark)] rounded-lg"
                  >
                    <div
                      className="w-2.5 h-2.5 rounded-full"
                      style={{ backgroundColor: info?.color || '#888' }}
                    />
                    <span className="text-xs text-gray-300">
                      {info?.label || quest.boss_name}
                    </span>
                    <span className="text-xs font-mono text-amber-400">
                      {remaining > 0 ? remaining : 0}
                    </span>
                  </div>
                );
              })}
              {getPlayerQuestProgress().length === 0 && (
                <span className="text-xs text-gray-500">No active quests</span>
              )}
            </div>
          </div>
        </div>

        {/* Start Button (if not started) */}
        {!isStarted && !isEnded && currentTask && (
          <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-8 text-center">
            <div className="text-5xl mb-4">🚀</div>
            <h2 className="text-xl font-bold text-white mb-2">Ready to Start?</h2>
            <p className="text-gray-400 mb-6">
              Click below to begin the party and start the timer.
            </p>
            <button
              onClick={handleStartStep}
              disabled={actionLoading}
              className="px-8 py-4 text-lg font-bold bg-gradient-to-r from-pink-600 to-rose-600 hover:from-pink-500 hover:to-rose-500 disabled:from-gray-600 disabled:to-gray-600 text-white rounded-xl transition-all shadow-lg"
            >
              {actionLoading ? 'Starting...' : 'Start Party'}
            </button>
          </div>
        )}

        {/* Main Boss Kill Counter */}
        {isStarted && currentTask && (
          <div
            className="bg-[var(--color-bg-card)] rounded-xl border-2 overflow-hidden"
            style={{ borderColor: bossInfo?.color || 'var(--color-border)' }}
          >
            <div
              className="px-6 py-4"
              style={{
                background: `linear-gradient(135deg, ${bossInfo?.color}22, transparent)`,
              }}
            >
              <div className="flex items-center justify-between flex-wrap gap-4">
                <div className="flex items-center gap-4">
                  <div
                    className="w-16 h-16 rounded-xl flex items-center justify-center text-3xl font-bold text-white"
                    style={{ backgroundColor: bossInfo?.color || '#888' }}
                  >
                    {(bossInfo?.label || currentTask.boss_name)[0].toUpperCase()}
                  </div>
                  <div>
                    <h2 className="text-2xl font-bold text-white">
                      {bossInfo?.label || currentTask.boss_name}
                    </h2>
                    <p className="text-sm text-gray-400">
                      {currentTask.key_holder
                        ? `Keys: ${currentTask.key_holder}`
                        : 'No keys assigned'}
                    </p>
                  </div>
                </div>

                {/* Kill counter display */}
                <div className="text-center">
                  <div className="text-4xl font-mono font-bold text-white">
                    {currentProgress?.kills_tracked || 0}
                    <span className="text-gray-500 text-2xl">
                      /{maxKillsNeeded}
                    </span>
                  </div>
                  <div className="text-xs text-gray-500">Kills Tracked</div>
                </div>
              </div>
            </div>

            {!isEnded && (
              <div className="px-6 py-4 bg-[var(--color-bg-dark)] border-t border-[var(--color-border)]">
                <div className="flex items-center justify-center gap-4 flex-wrap">
                  {/* Decrement buttons */}
                  <button
                    onClick={() => handleKillsDelta(-10)}
                    disabled={actionLoading}
                    className="w-14 h-14 text-xl font-bold bg-red-900/50 hover:bg-red-800/50 disabled:bg-gray-700 text-white rounded-xl transition-colors"
                  >
                    -10
                  </button>
                  <button
                    onClick={() => handleKillsDelta(-1)}
                    disabled={actionLoading}
                    className="w-14 h-14 text-2xl font-bold bg-red-700/50 hover:bg-red-600/50 disabled:bg-gray-700 text-white rounded-xl transition-colors"
                  >
                    -
                  </button>

                  {/* Direct input */}
                  <input
                    type="number"
                    value={killInput}
                    onChange={(e) => setKillInput(e.target.value)}
                    onBlur={() =>
                      handleKillsChange(parseInt(killInput) || 0)
                    }
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        handleKillsChange(parseInt(killInput) || 0);
                      }
                    }}
                    disabled={isEnded || actionLoading}
                    className="w-24 h-14 text-center text-2xl font-mono font-bold bg-[var(--color-bg-input)] border border-[var(--color-border)] rounded-xl text-white disabled:opacity-50"
                  />

                  {/* Increment buttons */}
                  <button
                    onClick={() => handleKillsDelta(1)}
                    disabled={actionLoading}
                    className="w-14 h-14 text-2xl font-bold bg-emerald-700/50 hover:bg-emerald-600/50 disabled:bg-gray-700 text-white rounded-xl transition-colors"
                  >
                    +
                  </button>
                  <button
                    onClick={() => handleKillsDelta(10)}
                    disabled={actionLoading}
                    className="w-14 h-14 text-xl font-bold bg-emerald-900/50 hover:bg-emerald-800/50 disabled:bg-gray-700 text-white rounded-xl transition-colors"
                  >
                    +10
                  </button>
                </div>
                <p className="text-center text-xs text-gray-500 mt-3">
                  Check loot stats in-game to see the current kill count
                </p>
                {conflictError && (
                  <div className="mt-3 px-4 py-2 bg-red-900/50 border border-red-500 rounded-lg text-center">
                    <p className="text-sm text-red-300">{conflictError}</p>
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        {/* Next Step Preview */}
        {isStarted && !isEnded && nextTask && (
          <div
            className={`bg-[var(--color-bg-card)] rounded-xl overflow-hidden transition-all ${
              leaderChanging
                ? 'border-2 border-amber-500 ring-2 ring-amber-500/30 animate-pulse'
                : 'border border-[var(--color-border)]'
            }`}
          >
            {leaderChanging && (
              <div className="bg-amber-600 px-4 py-2 text-center">
                <span className="text-white font-bold text-sm">
                  ⚠ LEADER CHANGE: {nextTask.key_holder}
                </span>
              </div>
            )}
            <div className="px-4 py-3 flex items-center justify-between flex-wrap gap-4">
              <div className="flex items-center gap-3">
                <div className="text-gray-500 text-sm font-medium">
                  NEXT:
                </div>
                <div
                  className="w-8 h-8 rounded-lg flex items-center justify-center text-sm font-bold text-white"
                  style={{ backgroundColor: nextBossInfo?.color || '#888' }}
                >
                  {(nextBossInfo?.label || nextTask.boss_name)[0].toUpperCase()}
                </div>
                <div>
                  <div className="font-medium text-white">
                    {nextBossInfo?.label || nextTask.boss_name}
                  </div>
                  <div className="text-xs text-gray-400">
                    {nextTask.kills} kills
                    {nextTask.key_holder && ` - Keys: ${nextTask.key_holder}`}
                  </div>
                </div>
              </div>
              <button
                onClick={handleNextStep}
                disabled={actionLoading}
                className="px-6 py-2 text-sm font-medium bg-violet-600 hover:bg-violet-700 disabled:bg-gray-600 text-white rounded-lg transition-colors"
              >
                {actionLoading ? 'Loading...' : 'Next Step'}
              </button>
            </div>
          </div>
        )}

        {/* Party complete message */}
        {isStarted && !isEnded && !nextTask && party.current_step_index >= allTasks.length - 1 && (
          <div className="bg-emerald-900/30 border border-emerald-700 rounded-xl p-6 text-center">
            <div className="text-4xl mb-4">🎉</div>
            <h3 className="text-lg font-bold text-emerald-400 mb-2">
              All Steps Complete!
            </h3>
            <p className="text-gray-400 text-sm">
              Great job! Click "End Party" below to finalize.
            </p>
          </div>
        )}

        {/* Key Management (for step leader) */}
        {isStarted && !isEnded && isCurrentLeader && currentTask?.key_holder && (
          <div className="bg-[var(--color-bg-card)] rounded-xl border border-amber-700/50 overflow-hidden">
            <div className="px-4 py-2 bg-amber-900/20 border-b border-amber-700/50">
              <span className="font-medium text-amber-400 text-sm">
                🔑 You are the key holder for this step
              </span>
            </div>
            <div className="p-4 flex items-center gap-4 flex-wrap">
              <label className="text-sm text-gray-400">Keys Used:</label>
              <input
                type="number"
                value={keysUsedInput}
                onChange={(e) => setKeysUsedInput(e.target.value)}
                className="w-24 px-3 py-2 bg-[var(--color-bg-input)] border border-[var(--color-border)] rounded-lg text-white text-center"
                min="0"
              />
              <button
                onClick={handleKeysChange}
                disabled={actionLoading}
                className="px-4 py-2 text-sm bg-amber-600 hover:bg-amber-700 disabled:bg-gray-600 text-white rounded-lg transition-colors"
              >
                Update
              </button>
            </div>
          </div>
        )}

        {/* Party Plan Timeline */}
        <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
          <div className="px-4 py-3 border-b border-[var(--color-border)]">
            <h3 className="font-semibold text-white flex items-center gap-2">
              <span>📋</span>
              Party Plan
            </h3>
          </div>
          <div className="p-4 space-y-2">
            {allTasks.map((task, index) => {
              const info = getBossInfo(task.boss_name);
              const progress = party.step_progress.find(
                (p) => p.step_index === index
              );
              const isCurrent = index === party.current_step_index;
              const isCompleted =
                progress?.completed_at || index < party.current_step_index;
              const isLeaderChange =
                index > 0 &&
                allTasks[index - 1]?.key_holder !== task.key_holder &&
                task.key_holder !== '';

              return (
                <div
                  key={index}
                  className={`flex items-center gap-3 px-3 py-2 rounded-lg transition-all ${
                    isCurrent
                      ? 'bg-violet-900/30 border border-violet-500'
                      : isCompleted
                      ? 'bg-[var(--color-bg-dark)] opacity-60'
                      : 'bg-[var(--color-bg-dark)]'
                  }`}
                >
                  {/* Step indicator */}
                  <div
                    className={`w-8 h-8 rounded-full flex items-center justify-center text-sm font-bold ${
                      isCompleted
                        ? 'bg-emerald-600 text-white'
                        : isCurrent
                        ? 'bg-violet-600 text-white'
                        : 'bg-[var(--color-bg-input)] text-gray-400'
                    }`}
                  >
                    {isCompleted ? '✓' : index + 1}
                  </div>

                  {/* Boss info */}
                  <div
                    className="w-3 h-3 rounded-full"
                    style={{ backgroundColor: info?.color || '#888' }}
                  />
                  <div className="flex-1">
                    <span
                      className={`font-medium ${
                        isCurrent ? 'text-white' : 'text-gray-300'
                      }`}
                    >
                      {info?.label || task.boss_name}
                    </span>
                    <span className="text-gray-500 ml-2 text-sm">
                      {task.kills} kills
                    </span>
                  </div>

                  {/* Leader change badge */}
                  {isLeaderChange && (
                    <span className="px-2 py-0.5 text-xs bg-amber-600 text-white rounded-full">
                      Leader: {task.key_holder}
                    </span>
                  )}

                  {/* Key holder */}
                  {!isLeaderChange && task.key_holder && (
                    <span className="text-xs text-gray-500">
                      {task.key_holder}
                    </span>
                  )}

                  {/* Progress */}
                  {progress && !isCompleted && (
                    <span className="text-sm font-mono text-amber-400">
                      {progress.kills_tracked}/{task.kills}
                    </span>
                  )}
                </div>
              );
            })}
            {allTasks.length === 0 && (
              <p className="text-gray-500 text-center py-4">No tasks in plan</p>
            )}
          </div>
        </div>

        {/* End Party Button */}
        {isStarted && !isEnded && (
          <div className="flex justify-center">
            <button
              onClick={() => setShowEndConfirm(true)}
              className="px-6 py-3 text-sm font-medium text-red-400 hover:text-white border border-red-700 hover:bg-red-700 rounded-lg transition-colors"
            >
              End Party
            </button>
          </div>
        )}

        {/* Ended State */}
        {isEnded && (
          <div className="bg-[var(--color-bg-card)] rounded-xl border border-gray-700 p-6 text-center">
            <div className="text-4xl mb-2">🔒</div>
            <h3 className="text-lg font-bold text-gray-400 mb-2">
              Party Ended
            </h3>
            <p className="text-gray-500 text-sm">
              This party has been completed and is now read-only.
            </p>
          </div>
        )}
      </div>

      {/* End Party Confirmation Modal */}
      {showEndConfirm && (
        <div className="fixed inset-0 bg-black/70 flex items-center justify-center p-4 z-50">
          <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] max-w-sm w-full p-6">
            <h3 className="text-lg font-semibold text-white mb-2">
              End Party?
            </h3>
            <p className="text-gray-400 text-sm mb-4">
              This will lock the party and make it read-only. All progress will
              be saved.
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setShowEndConfirm(false)}
                className="px-4 py-2 text-sm text-gray-400 hover:text-white border border-gray-600 rounded-lg transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleEndParty}
                disabled={actionLoading}
                className="px-4 py-2 text-sm font-medium bg-red-600 hover:bg-red-700 disabled:bg-gray-600 text-white rounded-lg transition-colors"
              >
                {actionLoading ? 'Ending...' : 'End Party'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

