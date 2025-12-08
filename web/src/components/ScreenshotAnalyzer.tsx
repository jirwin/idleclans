import { useState, useEffect, useCallback } from 'react';

interface BossResult {
  name: string;
  kills: number;
}

interface KeyResult {
  type: string;
  count: number;
}

interface AnalyzeQuestsResponse {
  bosses: BossResult[];
  applied: boolean;
  error?: string;
}

interface AnalyzeKeysResponse {
  keys: KeyResult[];
  applied: boolean;
  error?: string;
}

type PasteTarget = 'quest' | 'keys' | null;

export function ScreenshotAnalyzer() {
  const [questFile, setQuestFile] = useState<File | null>(null);
  const [keyFile, setKeyFile] = useState<File | null>(null);
  const [questPreview, setQuestPreview] = useState<string | null>(null);
  const [keyPreview, setKeyPreview] = useState<string | null>(null);
  const [questResult, setQuestResult] = useState<AnalyzeQuestsResponse | null>(null);
  const [keyResult, setKeyResult] = useState<AnalyzeKeysResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pasteTarget, setPasteTarget] = useState<PasteTarget>(null);

  // Handle image from clipboard or file
  const handleQuestImage = useCallback((file: File) => {
    setQuestFile(file);
    setQuestResult(null);
    setError(null);
    
    const reader = new FileReader();
    reader.onload = (e) => {
      setQuestPreview(e.target?.result as string);
    };
    reader.readAsDataURL(file);
  }, []);

  const handleKeyImage = useCallback((file: File) => {
    setKeyFile(file);
    setKeyResult(null);
    setError(null);
    
    const reader = new FileReader();
    reader.onload = (e) => {
      setKeyPreview(e.target?.result as string);
    };
    reader.readAsDataURL(file);
  }, []);

  // Global paste handler
  useEffect(() => {
    const handlePaste = (e: ClipboardEvent) => {
      const items = e.clipboardData?.items;
      if (!items) return;

      for (const item of items) {
        if (item.type.startsWith('image/')) {
          const file = item.getAsFile();
          if (file) {
            e.preventDefault();
            
            // Route to the appropriate handler based on paste target
            if (pasteTarget === 'quest') {
              handleQuestImage(file);
            } else if (pasteTarget === 'keys') {
              handleKeyImage(file);
            }
          }
          break;
        }
      }
    };

    document.addEventListener('paste', handlePaste);
    return () => document.removeEventListener('paste', handlePaste);
  }, [pasteTarget, handleQuestImage, handleKeyImage]);

  const handleQuestFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      handleQuestImage(file);
    }
  };

  const handleKeyFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      handleKeyImage(file);
    }
  };

  const analyzeQuests = async () => {
    if (!questFile) return;

    setLoading(true);
    setError(null);
    setQuestResult(null);

    try {
      const formData = new FormData();
      formData.append('image', questFile);

      // Use admin endpoint (no auth required)
      const response = await fetch('/api/admin/analyze/quests', {
        method: 'POST',
        credentials: 'include',
        body: formData,
      });

      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP ${response.status}`);
      }

      const data: AnalyzeQuestsResponse = await response.json();
      setQuestResult(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to analyze quest screenshot');
    } finally {
      setLoading(false);
    }
  };

  const analyzeKeys = async () => {
    if (!keyFile) return;

    setLoading(true);
    setError(null);
    setKeyResult(null);

    try {
      const formData = new FormData();
      formData.append('image', keyFile);

      // Use admin endpoint (no auth required)
      const response = await fetch('/api/admin/analyze/keys', {
        method: 'POST',
        credentials: 'include',
        body: formData,
      });

      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP ${response.status}`);
      }

      const data: AnalyzeKeysResponse = await response.json();
      setKeyResult(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to analyze key screenshot');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] p-4">
        <h2 className="text-xl font-bold text-white mb-4 flex items-center gap-2">
          <span className="text-2xl">📸</span>
          Screenshot Analyzer Test
        </h2>
        <p className="text-sm text-gray-400 mb-4">
          Test the LLM-powered screenshot analysis. Upload quest tracker or key inventory screenshots to extract data.
        </p>

        <div className="mb-4 p-3 bg-blue-900/20 border border-blue-700/50 rounded-lg">
          <p className="text-sm text-blue-300">
            <span className="font-medium">Note:</span> This is a test interface. Results are displayed but not automatically applied. 
            Use the public interface with authentication to auto-apply results to player data.
          </p>
        </div>

        <div className="mb-4 p-3 bg-violet-900/20 border border-violet-700/50 rounded-lg">
          <p className="text-sm text-violet-300">
            <span className="font-medium">💡 Tip:</span> Click a paste zone below, then press <kbd className="px-1.5 py-0.5 bg-violet-800/50 rounded text-xs">Ctrl+V</kbd> to paste an image from your clipboard.
          </p>
        </div>

        {error && (
          <div className="mb-4 p-3 bg-red-900/20 border border-red-700/50 rounded-lg">
            <p className="text-sm text-red-300">{error}</p>
          </div>
        )}
      </div>

      <div className="grid gap-6 md:grid-cols-2">
        {/* Quest Analyzer */}
        <div 
          className={`bg-[var(--color-bg-card)] rounded-xl border-2 p-4 cursor-pointer transition-all ${
            pasteTarget === 'quest' 
              ? 'border-violet-500 ring-2 ring-violet-500/30' 
              : 'border-[var(--color-border)] hover:border-violet-500/50'
          }`}
          onClick={() => setPasteTarget('quest')}
        >
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-semibold text-white">Quest Tracker</h3>
            {pasteTarget === 'quest' && (
              <span className="text-xs px-2 py-1 bg-violet-600 text-white rounded animate-pulse">
                📋 Ready to paste
              </span>
            )}
          </div>
          
          <div className="space-y-4">
            <div>
              <label className="block text-sm text-gray-300 mb-2">Upload or paste Quest Screenshot</label>
              <div className="flex items-center gap-2">
                <label className="flex-1 cursor-pointer" onClick={(e) => e.stopPropagation()}>
                  <input
                    type="file"
                    accept="image/*"
                    onChange={handleQuestFileChange}
                    className="hidden"
                  />
                  <div className="px-4 py-2 bg-[var(--color-bg-input)] border border-[var(--color-border)] rounded-lg hover:border-violet-500 transition-colors flex items-center justify-center gap-2">
                    <span className="text-sm">📤</span>
                    <span className="text-sm text-gray-300">
                      {questFile ? questFile.name : 'Choose file...'}
                    </span>
                  </div>
                </label>
              </div>
            </div>

            {questPreview && (
              <div className="space-y-2">
                <img
                  src={questPreview}
                  alt="Quest preview"
                  className="w-full rounded-lg border border-[var(--color-border)] max-h-64 object-contain bg-[var(--color-bg-dark)]"
                />
                <button
                  onClick={analyzeQuests}
                  disabled={loading}
                  className="w-full px-4 py-2 bg-violet-600 hover:bg-violet-700 text-white rounded-lg transition-colors disabled:opacity-50 flex items-center justify-center gap-2"
                >
                  {loading ? (
                    <>
                      <span className="animate-spin">⏳</span>
                      Analyzing...
                    </>
                  ) : (
                    '🔍 Analyze'
                  )}
                </button>
              </div>
            )}

            {questResult && (
              <div className="mt-4 p-3 bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)]">
                {questResult.error ? (
                  <div className="flex items-center gap-2 text-red-400">
                    <span>❌</span>
                    <span className="text-sm">{questResult.error}</span>
                  </div>
                ) : (
                  <div>
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-sm font-medium text-white">
                        Extracted {questResult.bosses.length} boss{questResult.bosses.length !== 1 ? 'es' : ''}
                      </span>
                      {questResult.applied && (
                        <span className="text-xs px-2 py-1 bg-emerald-900/50 text-emerald-400 rounded">
                          Applied
                        </span>
                      )}
                    </div>
                    <div className="space-y-1">
                      {questResult.bosses.map((boss) => (
                        <div
                          key={boss.name}
                          className="flex items-center justify-between text-sm py-1 px-2 bg-[var(--color-bg-card)] rounded"
                        >
                          <span className="text-gray-300 capitalize">{boss.name}</span>
                          <span className="text-violet-400 font-mono">{boss.kills} kills</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Key Analyzer */}
        <div 
          className={`bg-[var(--color-bg-card)] rounded-xl border-2 p-4 cursor-pointer transition-all ${
            pasteTarget === 'keys' 
              ? 'border-emerald-500 ring-2 ring-emerald-500/30' 
              : 'border-[var(--color-border)] hover:border-emerald-500/50'
          }`}
          onClick={() => setPasteTarget('keys')}
        >
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-semibold text-white">Key Inventory</h3>
            {pasteTarget === 'keys' && (
              <span className="text-xs px-2 py-1 bg-emerald-600 text-white rounded animate-pulse">
                📋 Ready to paste
              </span>
            )}
          </div>
          
          <div className="space-y-4">
            <div>
              <label className="block text-sm text-gray-300 mb-2">Upload or paste Key Screenshot</label>
              <div className="flex items-center gap-2">
                <label className="flex-1 cursor-pointer" onClick={(e) => e.stopPropagation()}>
                  <input
                    type="file"
                    accept="image/*"
                    onChange={handleKeyFileChange}
                    className="hidden"
                  />
                  <div className="px-4 py-2 bg-[var(--color-bg-input)] border border-[var(--color-border)] rounded-lg hover:border-emerald-500 transition-colors flex items-center justify-center gap-2">
                    <span className="text-sm">📤</span>
                    <span className="text-sm text-gray-300">
                      {keyFile ? keyFile.name : 'Choose file...'}
                    </span>
                  </div>
                </label>
              </div>
            </div>

            {keyPreview && (
              <div className="space-y-2">
                <img
                  src={keyPreview}
                  alt="Key preview"
                  className="w-full rounded-lg border border-[var(--color-border)] max-h-64 object-contain bg-[var(--color-bg-dark)]"
                />
                <button
                  onClick={analyzeKeys}
                  disabled={loading}
                  className="w-full px-4 py-2 bg-violet-600 hover:bg-violet-700 text-white rounded-lg transition-colors disabled:opacity-50 flex items-center justify-center gap-2"
                >
                  {loading ? (
                    <>
                      <span className="animate-spin">⏳</span>
                      Analyzing...
                    </>
                  ) : (
                    '🔍 Analyze'
                  )}
                </button>
              </div>
            )}

            {keyResult && (
              <div className="mt-4 p-3 bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)]">
                {keyResult.error ? (
                  <div className="flex items-center gap-2 text-red-400">
                    <span>❌</span>
                    <span className="text-sm">{keyResult.error}</span>
                  </div>
                ) : (
                  <div>
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-sm font-medium text-white">
                        Extracted {keyResult.keys.length} key type{keyResult.keys.length !== 1 ? 's' : ''}
                      </span>
                      {keyResult.applied && (
                        <span className="text-xs px-2 py-1 bg-emerald-900/50 text-emerald-400 rounded">
                          Applied
                        </span>
                      )}
                    </div>
                    <div className="space-y-1">
                      {keyResult.keys.map((key) => (
                        <div
                          key={key.type}
                          className="flex items-center justify-between text-sm py-1 px-2 bg-[var(--color-bg-card)] rounded"
                        >
                          <span className="text-gray-300 capitalize">{key.type}</span>
                          <span className="text-emerald-400 font-mono">{key.count}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

