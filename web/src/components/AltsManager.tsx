import { useState } from 'react';
import { addAlt, removeAlt } from '../api';

interface AltsManagerProps {
  alts: string[];
  onUpdate: () => void;
}

export function AltsManager({ alts, onUpdate }: AltsManagerProps) {
  const [showAddForm, setShowAddForm] = useState(false);
  const [newAltName, setNewAltName] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [removingAlt, setRemovingAlt] = useState<string | null>(null);

  const handleAddAlt = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newAltName.trim()) return;

    setLoading(true);
    setError(null);

    try {
      const result = await addAlt(newAltName.trim());
      if (result.error) {
        setError(result.error);
      } else {
        setNewAltName('');
        setShowAddForm(false);
        onUpdate();
      }
    } catch (err) {
      setError('Failed to add alt. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  const handleRemoveAlt = async (altName: string) => {
    if (!confirm(`Remove alt "${altName}"? Their quest and key data will remain.`)) {
      return;
    }

    setRemovingAlt(altName);
    try {
      const result = await removeAlt(altName);
      if (result.error) {
        alert(result.error);
      } else {
        onUpdate();
      }
    } catch (err) {
      alert('Failed to remove alt. Please try again.');
    } finally {
      setRemovingAlt(null);
    }
  };

  return (
    <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] overflow-hidden">
      <div className="px-4 py-3 border-b border-[var(--color-border)] flex items-center justify-between">
        <h3 className="font-semibold text-white flex items-center gap-2">
          <span className="text-lg">👥</span>
          Alt Characters
        </h3>
        {!showAddForm && (
          <button
            onClick={() => setShowAddForm(true)}
            className="px-3 py-1.5 text-xs font-medium text-violet-400 hover:text-violet-300 border border-violet-700 hover:border-violet-600 rounded-lg transition-colors"
          >
            + Add Alt
          </button>
        )}
      </div>

      <div className="p-4">
        {/* Add Alt Form */}
        {showAddForm && (
          <form onSubmit={handleAddAlt} className="mb-4 p-3 bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)]">
            <div className="flex gap-2">
              <input
                type="text"
                value={newAltName}
                onChange={(e) => setNewAltName(e.target.value)}
                placeholder="Alt character name"
                className="flex-1 px-3 py-2 text-sm bg-[var(--color-bg-input)] border border-[var(--color-border)] rounded-lg text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-violet-500"
                disabled={loading}
                autoFocus
              />
              <button
                type="submit"
                disabled={loading || !newAltName.trim()}
                className="px-4 py-2 text-sm font-medium bg-violet-600 hover:bg-violet-700 disabled:bg-gray-600 text-white rounded-lg transition-colors disabled:opacity-50"
              >
                {loading ? '...' : 'Add'}
              </button>
              <button
                type="button"
                onClick={() => {
                  setShowAddForm(false);
                  setNewAltName('');
                  setError(null);
                }}
                disabled={loading}
                className="px-4 py-2 text-sm font-medium bg-gray-600 hover:bg-gray-700 text-white rounded-lg transition-colors"
              >
                Cancel
              </button>
            </div>
            {error && (
              <p className="mt-2 text-sm text-red-400">{error}</p>
            )}
            <p className="mt-2 text-xs text-gray-500">
              The alt will be verified against the IdleClans API.
            </p>
          </form>
        )}

        {/* Alts List */}
        {alts.length === 0 ? (
          <p className="text-sm text-gray-500 text-center py-4">
            No alts registered yet.
            {!showAddForm && ' Click "Add Alt" to register one.'}
          </p>
        ) : (
          <div className="space-y-2">
            {alts.map((alt) => (
              <div
                key={alt}
                className="flex items-center justify-between px-3 py-2 bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)]"
              >
                <span className="text-sm font-medium text-gray-200">{alt}</span>
                <button
                  onClick={() => handleRemoveAlt(alt)}
                  disabled={removingAlt === alt}
                  className="p-1.5 text-gray-500 hover:text-red-400 transition-colors disabled:opacity-50"
                  title="Remove alt"
                >
                  {removingAlt === alt ? (
                    <div className="w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin" />
                  ) : (
                    <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  )}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

