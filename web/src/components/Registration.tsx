import { useState } from 'react';
import { registerPlayer } from '../api';

interface RegistrationProps {
  username: string;
  avatar: string;
  discordId: string;
  onSuccess: () => void;
  onLogout: () => void;
}

export function Registration({ username, avatar, discordId, onSuccess, onLogout }: RegistrationProps) {
  const [playerName, setPlayerName] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!playerName.trim()) {
      setError('Please enter your character name');
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const result = await registerPlayer(playerName.trim());
      if (result.success) {
        onSuccess();
      } else {
        setError(result.error || 'Registration failed');
      }
    } catch (err) {
      setError('Failed to register. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="max-w-md w-full">
        <div className="bg-[var(--color-bg-card)] rounded-2xl border border-[var(--color-border)] overflow-hidden shadow-xl">
          {/* Header */}
          <div className="bg-gradient-to-r from-violet-900/50 to-purple-900/50 p-6 text-center border-b border-[var(--color-border)]">
            <div className="w-16 h-16 mx-auto mb-4 rounded-full bg-gradient-to-br from-violet-600 to-purple-700 flex items-center justify-center text-3xl shadow-lg">
              ⚔️
            </div>
            <h1 className="text-xl font-bold text-white mb-1">Welcome to IdleClans Quest Manager</h1>
            <p className="text-sm text-gray-400">Link your character to get started</p>
          </div>

          {/* User info */}
          <div className="p-4 border-b border-[var(--color-border)] bg-[var(--color-bg-dark)]/50">
            <div className="flex items-center gap-3">
              {avatar && (
                <img
                  src={`https://cdn.discordapp.com/avatars/${discordId}/${avatar}.png`}
                  alt=""
                  className="w-10 h-10 rounded-full border-2 border-violet-500"
                />
              )}
              <div>
                <p className="text-sm font-medium text-white">Logged in as</p>
                <p className="text-sm text-violet-400">{username}</p>
              </div>
            </div>
          </div>

          {/* Form */}
          <form onSubmit={handleSubmit} className="p-6">
            <div className="mb-6">
              <label htmlFor="playerName" className="block text-sm font-medium text-gray-300 mb-2">
                IdleClans Character Name
              </label>
              <input
                type="text"
                id="playerName"
                value={playerName}
                onChange={(e) => setPlayerName(e.target.value)}
                placeholder="Enter your character name"
                className="w-full px-4 py-3 text-base bg-[var(--color-bg-dark)] border border-[var(--color-border)] rounded-lg text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-violet-500 focus:border-transparent"
                disabled={loading}
                autoFocus
              />
              <p className="mt-2 text-xs text-gray-500">
                Enter your exact character name from IdleClans. This will be verified.
              </p>
            </div>

            {error && (
              <div className="mb-4 p-3 bg-red-900/30 border border-red-700/50 rounded-lg">
                <p className="text-sm text-red-300">{error}</p>
              </div>
            )}

            <button
              type="submit"
              disabled={loading || !playerName.trim()}
              className="w-full py-3 px-4 bg-gradient-to-r from-violet-600 to-purple-600 hover:from-violet-500 hover:to-purple-500 disabled:from-gray-600 disabled:to-gray-600 text-white font-medium rounded-lg transition-all disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
            >
              {loading ? (
                <>
                  <div className="w-5 h-5 border-2 border-white border-t-transparent rounded-full animate-spin" />
                  Verifying...
                </>
              ) : (
                <>
                  <span>🔗</span>
                  Link Character
                </>
              )}
            </button>

            <div className="mt-4 text-center">
              <button
                type="button"
                onClick={onLogout}
                className="text-sm text-gray-500 hover:text-gray-300 transition-colors"
              >
                Sign out and use a different Discord account
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  );
}

