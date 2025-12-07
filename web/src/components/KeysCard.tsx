import { useState } from 'react';
import { KEY_TYPES } from '../types';

interface KeysCardProps {
  playerName: string;
  keys: Record<string, number>;
  onUpdateKeys: (playerName: string, keyType: string, count: number) => Promise<void>;
  isAdmin?: boolean;
}

export function KeysCard({ playerName, keys, onUpdateKeys, isAdmin }: KeysCardProps) {
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [saving, setSaving] = useState(false);

  const handleEdit = (keyType: string, currentValue: number) => {
    setEditingKey(keyType);
    setEditValue(currentValue.toString());
  };

  const handleSave = async (keyType: string) => {
    const value = parseInt(editValue, 10);
    if (isNaN(value) || value < 0) return;

    setSaving(true);
    try {
      await onUpdateKeys(playerName, keyType, value);
      setEditingKey(null);
    } catch (error) {
      console.error('Failed to update keys:', error);
    } finally {
      setSaving(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent, keyType: string) => {
    if (e.key === 'Enter') {
      handleSave(keyType);
    } else if (e.key === 'Escape') {
      setEditingKey(null);
    }
  };

  return (
    <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
      <div className="px-4 py-3 border-b border-[var(--color-border)] flex items-center justify-between">
        <h3 className="font-semibold text-white flex items-center gap-2">
          <span className="text-lg">🔑</span>
          {playerName}
          {isAdmin && <span className="text-xs text-gray-500">(Admin)</span>}
        </h3>
        <span className="text-xs text-gray-500">Boss Keys</span>
      </div>
      
      <div className="divide-y divide-[var(--color-border)]">
        {KEY_TYPES.map((keyInfo) => {
          const count = keys[keyInfo.type] ?? 0;
          const isEditing = editingKey === keyInfo.type;
          
          return (
            <div
              key={keyInfo.type}
              className="px-4 py-3 flex items-center justify-between hover:bg-[var(--color-bg-input)] transition-colors"
            >
              <div className="flex items-center gap-3">
                <div
                  className="w-3 h-3 rounded-sm"
                  style={{ backgroundColor: keyInfo.color }}
                />
                <span className="text-sm font-medium text-gray-200">{keyInfo.label}</span>
              </div>
              
              <div className="flex items-center gap-1">
                {isEditing ? (
                  <div className="flex items-center gap-1">
                    <input
                      type="number"
                      inputMode="numeric"
                      pattern="[0-9]*"
                      value={editValue}
                      onChange={(e) => setEditValue(e.target.value)}
                      onKeyDown={(e) => handleKeyDown(e, keyInfo.type)}
                      onFocus={(e) => e.target.select()}
                      className="w-20 px-3 py-2 text-base bg-[var(--color-bg-dark)] border border-violet-500 rounded-lg text-white text-center focus:outline-none focus:ring-2 focus:ring-violet-500"
                      min="0"
                      autoFocus
                      disabled={saving}
                    />
                    <button
                      onClick={() => handleSave(keyInfo.type)}
                      disabled={saving}
                      className="p-2 min-w-[44px] min-h-[44px] flex items-center justify-center text-sm bg-violet-600 hover:bg-violet-700 active:bg-violet-800 text-white rounded-lg transition-colors disabled:opacity-50"
                    >
                      {saving ? '...' : '✓'}
                    </button>
                    <button
                      onClick={() => setEditingKey(null)}
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
                        count > 0 ? 'text-emerald-400' : 'text-gray-500'
                      }`}
                    >
                      {count}
                    </span>
                    <button
                      onClick={() => handleEdit(keyInfo.type, count)}
                      className="p-3 -m-2 text-gray-500 hover:text-violet-400 active:text-violet-500 transition-colors"
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

