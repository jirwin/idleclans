import { useState } from 'react';
import type { Quest } from '../types';
import { BOSSES } from '../types';

interface QuestCardProps {
  playerName: string;
  quests: Quest[];
  onUpdateQuest: (playerName: string, boss: string, kills: number) => Promise<void>;
  isAdmin?: boolean;
}

export function QuestCard({ playerName, quests, onUpdateQuest, isAdmin }: QuestCardProps) {
  const [editingBoss, setEditingBoss] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [saving, setSaving] = useState(false);
  const [completingBoss, setCompletingBoss] = useState<string | null>(null);

  const getQuestForBoss = (bossName: string) => {
    return quests.find((q) => q.boss_name === bossName);
  };

  const handleEdit = (bossName: string, currentValue: number) => {
    setEditingBoss(bossName);
    setEditValue(currentValue.toString());
  };

  const handleSave = async (bossName: string) => {
    const value = parseInt(editValue, 10);
    if (isNaN(value) || value < 0) return;

    setSaving(true);
    try {
      await onUpdateQuest(playerName, bossName, value);
      setEditingBoss(null);
    } catch (error) {
      console.error('Failed to update quest:', error);
    } finally {
      setSaving(false);
    }
  };

  const handleComplete = async (bossName: string) => {
    setCompletingBoss(bossName);
    try {
      await onUpdateQuest(playerName, bossName, 0);
    } catch (error) {
      console.error('Failed to complete quest:', error);
    } finally {
      setCompletingBoss(null);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent, bossName: string) => {
    if (e.key === 'Enter') {
      handleSave(bossName);
    } else if (e.key === 'Escape') {
      setEditingBoss(null);
    }
  };

  return (
    <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
      <div className="px-4 py-3 border-b border-[var(--color-border)] flex items-center justify-between">
        <h3 className="font-semibold text-white flex items-center gap-2">
          <span className="text-lg">⚔️</span>
          {playerName}
          {isAdmin && <span className="text-xs text-gray-500">(Admin)</span>}
        </h3>
        <span className="text-xs text-gray-500">Weekly Quests</span>
      </div>
      
      <div className="divide-y divide-[var(--color-border)]">
        {BOSSES.map((boss) => {
          const quest = getQuestForBoss(boss.name);
          const remaining = quest ? quest.required_kills - quest.current_kills : 0;
          const isEditing = editingBoss === boss.name;
          
          return (
            <div
              key={boss.name}
              className="px-4 py-3 flex items-center justify-between hover:bg-[var(--color-bg-input)] transition-colors"
            >
              <div className="flex items-center gap-3">
                <div
                  className="w-3 h-3 rounded-full"
                  style={{ backgroundColor: boss.color }}
                />
                <span className="text-sm font-medium text-gray-200">{boss.label}</span>
              </div>
              
              <div className="flex items-center gap-2">
                {isEditing ? (
                  <div className="flex items-center gap-1">
                    <input
                      type="number"
                      inputMode="numeric"
                      pattern="[0-9]*"
                      value={editValue}
                      onChange={(e) => setEditValue(e.target.value)}
                      onKeyDown={(e) => handleKeyDown(e, boss.name)}
                      onFocus={(e) => e.target.select()}
                      className="w-20 px-3 py-2 text-base bg-[var(--color-bg-dark)] border border-violet-500 rounded-lg text-white text-center focus:outline-none focus:ring-2 focus:ring-violet-500"
                      min="0"
                      autoFocus
                      disabled={saving}
                    />
                    <button
                      onClick={() => handleSave(boss.name)}
                      disabled={saving}
                      className="p-2 min-w-[44px] min-h-[44px] flex items-center justify-center text-sm bg-violet-600 hover:bg-violet-700 active:bg-violet-800 text-white rounded-lg transition-colors disabled:opacity-50"
                    >
                      {saving ? '...' : '✓'}
                    </button>
                    <button
                      onClick={() => setEditingBoss(null)}
                      disabled={saving}
                      className="p-2 min-w-[44px] min-h-[44px] flex items-center justify-center text-sm bg-gray-600 hover:bg-gray-700 active:bg-gray-800 text-white rounded-lg transition-colors disabled:opacity-50"
                    >
                      ✕
                    </button>
                  </div>
                ) : (
                  <div className="flex items-center gap-1">
                    <span
                      className={`text-sm font-mono min-w-[2rem] text-right ${
                        remaining > 0 ? 'text-amber-400' : 'text-gray-500'
                      }`}
                    >
                      {remaining > 0 ? remaining : '—'}
                    </span>
                    {remaining > 0 && (
                      <button
                        onClick={() => handleComplete(boss.name)}
                        disabled={completingBoss === boss.name}
                        className="p-2 text-gray-500 hover:text-emerald-400 active:text-emerald-500 transition-colors disabled:opacity-50"
                        title="Mark as complete"
                      >
                        {completingBoss === boss.name ? (
                          <div className="w-5 h-5 border-2 border-current border-t-transparent rounded-full animate-spin" />
                        ) : (
                          <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                          </svg>
                        )}
                      </button>
                    )}
                    <button
                      onClick={() => handleEdit(boss.name, quest?.required_kills ?? 0)}
                      className="p-2 text-gray-500 hover:text-violet-400 active:text-violet-500 transition-colors"
                      title="Edit"
                    >
                      <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
                      </svg>
                    </button>
                  </div>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

