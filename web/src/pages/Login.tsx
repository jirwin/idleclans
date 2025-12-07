import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { fetchUserData } from '../api';

export function Login() {
  const navigate = useNavigate();

  useEffect(() => {
    // Check if already logged in
    fetchUserData()
      .then(() => {
        navigate('/dashboard');
      })
      .catch(() => {
        // Not logged in, stay on login page
      });
  }, [navigate]);

  const handleLogin = () => {
    window.location.href = '/api/auth/login';
  };

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="max-w-md w-full">
        {/* Logo/Header */}
        <div className="text-center mb-12">
          <div className="inline-flex items-center justify-center w-20 h-20 rounded-2xl bg-gradient-to-br from-violet-600 to-purple-900 mb-6 shadow-lg shadow-violet-500/20">
            <svg className="w-10 h-10 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <h1 className="text-3xl font-bold text-white mb-2 tracking-tight">
            IdleClans Quest Manager
          </h1>
          <p className="text-gray-400 text-sm">
            Track your weekly quests and boss keys
          </p>
        </div>

        {/* Login Card */}
        <div className="bg-[var(--color-bg-card)] rounded-2xl p-8 border border-[var(--color-border)] shadow-xl">
          <div className="space-y-6">
            <div className="text-center">
              <h2 className="text-xl font-semibold text-white mb-2">Welcome Back</h2>
              <p className="text-gray-400 text-sm">
                Sign in with your Discord account to manage your quests
              </p>
            </div>

            <button
              onClick={handleLogin}
              className="w-full flex items-center justify-center gap-3 bg-[#5865F2] hover:bg-[#4752C4] text-white font-medium py-3 px-4 rounded-xl transition-all duration-200 hover:shadow-lg hover:shadow-[#5865F2]/20 hover:-translate-y-0.5"
            >
              <svg className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor">
                <path d="M20.317 4.37a19.791 19.791 0 0 0-4.885-1.515a.074.074 0 0 0-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 0 0-5.487 0a12.64 12.64 0 0 0-.617-1.25a.077.077 0 0 0-.079-.037A19.736 19.736 0 0 0 3.677 4.37a.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 0 0 .031.057a19.9 19.9 0 0 0 5.993 3.03a.078.078 0 0 0 .084-.028a14.09 14.09 0 0 0 1.226-1.994a.076.076 0 0 0-.041-.106a13.107 13.107 0 0 1-1.872-.892a.077.077 0 0 1-.008-.128a10.2 10.2 0 0 0 .372-.292a.074.074 0 0 1 .077-.01c3.928 1.793 8.18 1.793 12.062 0a.074.074 0 0 1 .078.01c.12.098.246.198.373.292a.077.077 0 0 1-.006.127a12.299 12.299 0 0 1-1.873.892a.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 0 0 .084.028a19.839 19.839 0 0 0 6.002-3.03a.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 0 0-.031-.03zM8.02 15.33c-1.183 0-2.157-1.085-2.157-2.419c0-1.333.956-2.419 2.157-2.419c1.21 0 2.176 1.096 2.157 2.42c0 1.333-.956 2.418-2.157 2.418zm7.975 0c-1.183 0-2.157-1.085-2.157-2.419c0-1.333.955-2.419 2.157-2.419c1.21 0 2.176 1.096 2.157 2.42c0 1.333-.946 2.418-2.157 2.418z" />
              </svg>
              Sign in with Discord
            </button>

            <div className="text-center">
              <p className="text-xs text-gray-500">
                Only your Discord ID is used to link your characters.
                <br />
                We don't access your messages or servers.
              </p>
            </div>
          </div>
        </div>

        {/* Features */}
        <div className="mt-8 grid grid-cols-3 gap-4 text-center">
          <div className="p-4">
            <div className="text-violet-400 text-2xl mb-2">⚔️</div>
            <div className="text-xs text-gray-400">Track Quests</div>
          </div>
          <div className="p-4">
            <div className="text-violet-400 text-2xl mb-2">🔑</div>
            <div className="text-xs text-gray-400">Manage Keys</div>
          </div>
          <div className="p-4">
            <div className="text-violet-400 text-2xl mb-2">👥</div>
            <div className="text-xs text-gray-400">Alt Support</div>
          </div>
        </div>
      </div>
    </div>
  );
}

