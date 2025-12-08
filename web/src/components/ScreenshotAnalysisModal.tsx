import { useState, useMemo } from 'react';
import { BOSSES, KEY_TYPES } from '../types';
import type { Quest } from '../types';
import type { AnalyzedBoss, AnalyzedKey } from '../api';

interface QuestModalProps {
  type: 'quests';
  imagePreview: string;
  analysisResults: AnalyzedBoss[];
  currentData: Quest[];
  playerName: string;
  onApply: (updates: Array<{ boss: string; kills: number }>) => Promise<void>;
  onClose: () => void;
}

interface KeysModalProps {
  type: 'keys';
  imagePreview: string;
  analysisResults: AnalyzedKey[];
  currentData: Record<string, number>;
  playerName: string;
  onApply: (updates: Array<{ keyType: string; count: number }>) => Promise<void>;
  onClose: () => void;
}

type ScreenshotAnalysisModalProps = QuestModalProps | KeysModalProps;

export function ScreenshotAnalysisModal(props: ScreenshotAnalysisModalProps) {
  const { type } = props;
  const [saving, setSaving] = useState(false);

  if (type === 'quests') {
    return (
      <QuestModal
        {...props as QuestModalProps}
        saving={saving}
        setSaving={setSaving}
      />
    );
  }

  return (
    <KeysModal
      {...props as KeysModalProps}
      saving={saving}
      setSaving={setSaving}
    />
  );
}

interface QuestModalInternalProps extends QuestModalProps {
  saving: boolean;
  setSaving: (saving: boolean) => void;
}

function QuestModal({
  imagePreview,
  analysisResults,
  currentData,
  playerName,
  onApply,
  onClose,
  saving,
  setSaving,
}: QuestModalInternalProps) {
  // Build initial values from analysis results, falling back to current data
  const initialValues = useMemo(() => {
    const values: Record<string, number> = {};
    
    // Start with current data
    for (const quest of currentData) {
      values[quest.boss_name] = quest.required_kills;
    }
    
    // Override with analysis results
    for (const result of analysisResults) {
      values[result.name] = result.kills;
    }
    
    return values;
  }, [analysisResults, currentData]);

  // Track which values came from analysis
  const analysisValues = useMemo(() => {
    const values: Record<string, number> = {};
    for (const result of analysisResults) {
      values[result.name] = result.kills;
    }
    return values;
  }, [analysisResults]);

  const [editedValues, setEditedValues] = useState<Record<string, number>>(initialValues);

  // Calculate changes
  const changes = useMemo(() => {
    const result: Array<{ boss: string; kills: number; isNew: boolean }> = [];
    
    for (const boss of BOSSES) {
      const currentQuest = currentData.find(q => q.boss_name === boss.name);
      const currentValue = currentQuest?.required_kills ?? 0;
      const newValue = editedValues[boss.name] ?? 0;
      
      if (newValue !== currentValue) {
        result.push({
          boss: boss.name,
          kills: newValue,
          isNew: boss.name in analysisValues,
        });
      }
    }
    
    return result;
  }, [currentData, editedValues, analysisValues]);

  const handleValueChange = (bossName: string, value: string) => {
    const numValue = parseInt(value, 10);
    setEditedValues(prev => ({
      ...prev,
      [bossName]: isNaN(numValue) ? 0 : Math.max(0, numValue),
    }));
  };

  const handleReset = (bossName: string) => {
    if (bossName in analysisValues) {
      setEditedValues(prev => ({
        ...prev,
        [bossName]: analysisValues[bossName],
      }));
    }
  };

  const handleApply = async () => {
    if (changes.length === 0) {
      onClose();
      return;
    }

    setSaving(true);
    try {
      await onApply(changes.map(c => ({ boss: c.boss, kills: c.kills })));
      onClose();
    } catch (error) {
      console.error('Failed to apply changes:', error);
    } finally {
      setSaving(false);
    }
  };

  return (
    <ModalWrapper onClose={onClose}>
      <div className="flex flex-col lg:flex-row gap-4 max-h-[80vh]">
        {/* Image Preview */}
        <div className="lg:w-1/2 flex-shrink-0">
          <h3 className="text-sm font-medium text-gray-400 mb-2">Screenshot Reference</h3>
          <div className="bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)] p-2 overflow-auto max-h-[60vh]">
            <img
              src={imagePreview}
              alt="Analyzed screenshot"
              className="w-full h-auto rounded"
            />
          </div>
        </div>

        {/* Editable Values */}
        <div className="lg:w-1/2 flex flex-col min-h-0">
          <div className="flex items-center justify-between mb-2">
            <h3 className="text-sm font-medium text-gray-400">
              Quest Values for {playerName}
            </h3>
            <span className="text-xs text-violet-400">
              {analysisResults.length} detected
            </span>
          </div>
          
          <div className="flex-1 overflow-auto bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)]">
            <div className="divide-y divide-[var(--color-border)]">
              {BOSSES.map((boss) => {
                const currentQuest = currentData.find(q => q.boss_name === boss.name);
                const currentValue = currentQuest?.required_kills ?? 0;
                const editedValue = editedValues[boss.name] ?? 0;
                const analysisValue = analysisValues[boss.name];
                const hasAnalysis = boss.name in analysisValues;
                const isModified = hasAnalysis && editedValue !== analysisValue;
                const willChange = editedValue !== currentValue;

                return (
                  <div
                    key={boss.name}
                    className={`px-3 py-2 flex items-center justify-between gap-2 ${
                      hasAnalysis ? 'bg-violet-900/20' : ''
                    } ${isModified ? 'bg-amber-900/20' : ''}`}
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <div
                        className="w-3 h-3 rounded-full flex-shrink-0"
                        style={{ backgroundColor: boss.color }}
                      />
                      <span className="text-sm font-medium text-gray-200 truncate">
                        {boss.label}
                      </span>
                      {hasAnalysis && (
                        <span className="text-xs px-1.5 py-0.5 bg-violet-600/50 text-violet-300 rounded flex-shrink-0">
                          detected
                        </span>
                      )}
                    </div>
                    
                    <div className="flex items-center gap-2">
                      {willChange && (
                        <span className="text-xs text-gray-500">
                          was {currentValue}
                        </span>
                      )}
                      <input
                        type="number"
                        value={editedValue}
                        onChange={(e) => handleValueChange(boss.name, e.target.value)}
                        className={`w-20 px-2 py-1 text-sm bg-[var(--color-bg-input)] border rounded text-white text-center focus:outline-none focus:ring-1 focus:ring-violet-500 ${
                          willChange ? 'border-violet-500' : 'border-[var(--color-border)]'
                        }`}
                        min="0"
                      />
                      {/* Reset to detected value if modified */}
                      {isModified && (
                        <button
                          onClick={() => handleReset(boss.name)}
                          className="p-1 text-amber-400 hover:text-amber-300 transition-colors"
                          title={`Reset to detected value (${analysisValue})`}
                        >
                          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                          </svg>
                        </button>
                      )}
                      {/* Undo detected change - reset to original value */}
                      {hasAnalysis && !isModified && willChange && (
                        <button
                          onClick={() => handleValueChange(boss.name, currentValue.toString())}
                          className="p-1 text-red-400 hover:text-red-300 transition-colors"
                          title={`Undo detection - restore original value (${currentValue})`}
                        >
                          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                          </svg>
                        </button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          {/* Footer */}
          <div className="mt-4 flex items-center justify-between">
            <span className="text-sm text-gray-400">
              {changes.length > 0 ? (
                <>{changes.length} value{changes.length !== 1 ? 's' : ''} will be updated</>
              ) : (
                'No changes'
              )}
            </span>
            <div className="flex gap-2">
              <button
                onClick={onClose}
                disabled={saving}
                className="px-4 py-2 text-sm bg-gray-600 hover:bg-gray-700 text-white rounded-lg transition-colors disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                onClick={handleApply}
                disabled={saving || changes.length === 0}
                className="px-4 py-2 text-sm bg-violet-600 hover:bg-violet-700 text-white rounded-lg transition-colors disabled:opacity-50 flex items-center gap-2"
              >
                {saving ? (
                  <>
                    <div className="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin" />
                    Applying...
                  </>
                ) : (
                  'Apply Changes'
                )}
              </button>
            </div>
          </div>
        </div>
      </div>
    </ModalWrapper>
  );
}

interface KeysModalInternalProps extends KeysModalProps {
  saving: boolean;
  setSaving: (saving: boolean) => void;
}

function KeysModal({
  imagePreview,
  analysisResults,
  currentData,
  playerName,
  onApply,
  onClose,
  saving,
  setSaving,
}: KeysModalInternalProps) {
  // Build initial values from analysis results, falling back to current data
  const initialValues = useMemo(() => {
    const values: Record<string, number> = {};
    
    // Start with current data
    for (const [keyType, count] of Object.entries(currentData)) {
      values[keyType] = count;
    }
    
    // Override with analysis results
    for (const result of analysisResults) {
      values[result.type] = result.count;
    }
    
    return values;
  }, [analysisResults, currentData]);

  // Track which values came from analysis
  const analysisValues = useMemo(() => {
    const values: Record<string, number> = {};
    for (const result of analysisResults) {
      values[result.type] = result.count;
    }
    return values;
  }, [analysisResults]);

  const [editedValues, setEditedValues] = useState<Record<string, number>>(initialValues);

  // Calculate changes
  const changes = useMemo(() => {
    const result: Array<{ keyType: string; count: number; isNew: boolean }> = [];
    
    for (const keyInfo of KEY_TYPES) {
      const currentValue = currentData[keyInfo.type] ?? 0;
      const newValue = editedValues[keyInfo.type] ?? 0;
      
      if (newValue !== currentValue) {
        result.push({
          keyType: keyInfo.type,
          count: newValue,
          isNew: keyInfo.type in analysisValues,
        });
      }
    }
    
    return result;
  }, [currentData, editedValues, analysisValues]);

  const handleValueChange = (keyType: string, value: string) => {
    const numValue = parseInt(value, 10);
    setEditedValues(prev => ({
      ...prev,
      [keyType]: isNaN(numValue) ? 0 : Math.max(0, numValue),
    }));
  };

  const handleReset = (keyType: string) => {
    if (keyType in analysisValues) {
      setEditedValues(prev => ({
        ...prev,
        [keyType]: analysisValues[keyType],
      }));
    }
  };

  const handleApply = async () => {
    if (changes.length === 0) {
      onClose();
      return;
    }

    setSaving(true);
    try {
      await onApply(changes.map(c => ({ keyType: c.keyType, count: c.count })));
      onClose();
    } catch (error) {
      console.error('Failed to apply changes:', error);
    } finally {
      setSaving(false);
    }
  };

  return (
    <ModalWrapper onClose={onClose}>
      <div className="flex flex-col lg:flex-row gap-4 max-h-[80vh]">
        {/* Image Preview */}
        <div className="lg:w-1/2 flex-shrink-0">
          <h3 className="text-sm font-medium text-gray-400 mb-2">Screenshot Reference</h3>
          <div className="bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)] p-2 overflow-auto max-h-[60vh]">
            <img
              src={imagePreview}
              alt="Analyzed screenshot"
              className="w-full h-auto rounded"
            />
          </div>
        </div>

        {/* Editable Values */}
        <div className="lg:w-1/2 flex flex-col min-h-0">
          <div className="flex items-center justify-between mb-2">
            <h3 className="text-sm font-medium text-gray-400">
              Key Values for {playerName}
            </h3>
            <span className="text-xs text-emerald-400">
              {analysisResults.length} detected
            </span>
          </div>
          
          <div className="flex-1 overflow-auto bg-[var(--color-bg-dark)] rounded-lg border border-[var(--color-border)]">
            <div className="divide-y divide-[var(--color-border)]">
              {KEY_TYPES.map((keyInfo) => {
                const currentValue = currentData[keyInfo.type] ?? 0;
                const editedValue = editedValues[keyInfo.type] ?? 0;
                const analysisValue = analysisValues[keyInfo.type];
                const hasAnalysis = keyInfo.type in analysisValues;
                const isModified = hasAnalysis && editedValue !== analysisValue;
                const willChange = editedValue !== currentValue;

                return (
                  <div
                    key={keyInfo.type}
                    className={`px-3 py-2 flex items-center justify-between gap-2 ${
                      hasAnalysis ? 'bg-emerald-900/20' : ''
                    } ${isModified ? 'bg-amber-900/20' : ''}`}
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <div
                        className="w-3 h-3 rounded-sm flex-shrink-0"
                        style={{ backgroundColor: keyInfo.color }}
                      />
                      <span className="text-sm font-medium text-gray-200 truncate">
                        {keyInfo.label}
                      </span>
                      {hasAnalysis && (
                        <span className="text-xs px-1.5 py-0.5 bg-emerald-600/50 text-emerald-300 rounded flex-shrink-0">
                          detected
                        </span>
                      )}
                    </div>
                    
                    <div className="flex items-center gap-2">
                      {willChange && (
                        <span className="text-xs text-gray-500">
                          was {currentValue}
                        </span>
                      )}
                      <input
                        type="number"
                        value={editedValue}
                        onChange={(e) => handleValueChange(keyInfo.type, e.target.value)}
                        className={`w-20 px-2 py-1 text-sm bg-[var(--color-bg-input)] border rounded text-white text-center focus:outline-none focus:ring-1 focus:ring-emerald-500 ${
                          willChange ? 'border-emerald-500' : 'border-[var(--color-border)]'
                        }`}
                        min="0"
                      />
                      {/* Reset to detected value if modified */}
                      {isModified && (
                        <button
                          onClick={() => handleReset(keyInfo.type)}
                          className="p-1 text-amber-400 hover:text-amber-300 transition-colors"
                          title={`Reset to detected value (${analysisValue})`}
                        >
                          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                          </svg>
                        </button>
                      )}
                      {/* Undo detected change - reset to original value */}
                      {hasAnalysis && !isModified && willChange && (
                        <button
                          onClick={() => handleValueChange(keyInfo.type, currentValue.toString())}
                          className="p-1 text-red-400 hover:text-red-300 transition-colors"
                          title={`Undo detection - restore original value (${currentValue})`}
                        >
                          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                          </svg>
                        </button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          {/* Footer */}
          <div className="mt-4 flex items-center justify-between">
            <span className="text-sm text-gray-400">
              {changes.length > 0 ? (
                <>{changes.length} value{changes.length !== 1 ? 's' : ''} will be updated</>
              ) : (
                'No changes'
              )}
            </span>
            <div className="flex gap-2">
              <button
                onClick={onClose}
                disabled={saving}
                className="px-4 py-2 text-sm bg-gray-600 hover:bg-gray-700 text-white rounded-lg transition-colors disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                onClick={handleApply}
                disabled={saving || changes.length === 0}
                className="px-4 py-2 text-sm bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg transition-colors disabled:opacity-50 flex items-center gap-2"
              >
                {saving ? (
                  <>
                    <div className="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin" />
                    Applying...
                  </>
                ) : (
                  'Apply Changes'
                )}
              </button>
            </div>
          </div>
        </div>
      </div>
    </ModalWrapper>
  );
}

// Shared modal wrapper component
function ModalWrapper({ children, onClose }: { children: React.ReactNode; onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/70 backdrop-blur-sm"
        onClick={onClose}
      />
      
      {/* Modal Content */}
      <div className="relative bg-[var(--color-bg-card)] rounded-xl border border-[var(--color-border)] shadow-2xl w-full max-w-4xl max-h-[90vh] overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-[var(--color-border)]">
          <h2 className="text-lg font-semibold text-white flex items-center gap-2">
            <span>📸</span>
            Review Screenshot Analysis
          </h2>
          <button
            onClick={onClose}
            className="p-1 text-gray-400 hover:text-white transition-colors"
          >
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        
        {/* Body */}
        <div className="p-4 overflow-auto">
          {children}
        </div>
      </div>
    </div>
  );
}

