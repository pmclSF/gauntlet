import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getBaselines, getBaselineDiff } from '../api/client';
import { EmptyState } from '../components/EmptyState';
import { Loading } from '../components/Loading';
import type { BaselineContract } from '../api/types';

export function Baselines() {
  const [suite, setSuite] = useState('smoke');
  const [activeSuite, setActiveSuite] = useState('smoke');

  const { data: baselines, isLoading } = useQuery({
    queryKey: ['baselines', activeSuite],
    queryFn: () => getBaselines(activeSuite),
  });

  const [expandedScenario, setExpandedScenario] = useState<string | null>(null);

  const handleLoad = () => setActiveSuite(suite);

  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-6">Baselines</h1>

      {/* Suite selector */}
      <div className="bg-white border rounded-lg p-4 mb-6">
        <div className="flex gap-4 items-end">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Suite</label>
            <input
              type="text"
              value={suite}
              onChange={(e) => setSuite(e.target.value)}
              className="border rounded px-3 py-1.5 text-sm"
              placeholder="smoke"
              onKeyDown={(e) => e.key === 'Enter' && handleLoad()}
            />
          </div>
          <button
            onClick={handleLoad}
            className="px-4 py-1.5 bg-gray-900 text-white text-sm rounded hover:bg-gray-800"
          >
            Load
          </button>
        </div>
      </div>

      {isLoading && <Loading message="Loading baselines..." />}

      {!isLoading && (!baselines || baselines.length === 0) && (
        <EmptyState
          title="No baselines for this suite"
          description={`Run \`gauntlet baseline --suite ${activeSuite}\` to generate baselines.`}
        />
      )}

      {baselines && baselines.length > 0 && (
        <div className="space-y-3">
          {baselines.map((b) => (
            <BaselineCard
              key={b.scenario}
              baseline={b}
              suite={activeSuite}
              isExpanded={expandedScenario === b.scenario}
              onToggle={() =>
                setExpandedScenario(expandedScenario === b.scenario ? null : b.scenario)
              }
            />
          ))}
        </div>
      )}
    </div>
  );
}

function BaselineCard({
  baseline,
  suite,
  isExpanded,
  onToggle,
}: {
  baseline: BaselineContract;
  suite: string;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const { data: detail } = useQuery({
    queryKey: ['baseline-diff', suite, baseline.scenario],
    queryFn: () => getBaselineDiff(suite, baseline.scenario),
    enabled: isExpanded,
  });

  const toolSeq = baseline.tool_sequence?.required ?? [];
  const requiredFields = baseline.output?.required_fields ?? [];
  const forbidden = baseline.output?.forbidden_content ?? [];

  return (
    <div className="bg-white border rounded-lg shadow-sm overflow-hidden">
      <div
        className="p-4 cursor-pointer hover:bg-gray-50 flex items-start justify-between"
        onClick={onToggle}
      >
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-2">
            <span className="text-gray-400 mr-1">{isExpanded ? '\u25BC' : '\u25B6'}</span>
            <span className="font-medium">{baseline.scenario}</span>
          </div>

          {/* Tool sequence */}
          {toolSeq.length > 0 && (
            <div className="flex items-center gap-1 flex-wrap mb-1">
              {toolSeq.map((tool, i) => (
                <span key={i} className="flex items-center">
                  {i > 0 && <span className="text-gray-400 mx-1">&rarr;</span>}
                  <code className="text-xs bg-blue-50 text-blue-700 px-1.5 py-0.5 rounded">{tool}</code>
                </span>
              ))}
            </div>
          )}

          <div className="flex gap-1 flex-wrap">
            {requiredFields.map((f) => (
              <span key={f} className="text-xs bg-green-50 text-green-700 px-1.5 py-0.5 rounded font-mono">{f}</span>
            ))}
            {forbidden.map((c) => (
              <span key={c} className="text-xs bg-red-50 text-red-700 px-1.5 py-0.5 rounded">{c}</span>
            ))}
          </div>
        </div>
      </div>

      {isExpanded && detail && (
        <div className="px-4 pb-4 border-t bg-gray-50">
          <div className="pt-3 space-y-3">
            {detail.output?.schema && (
              <div>
                <h3 className="text-xs font-semibold text-gray-500 mb-1">Output Schema</h3>
                <pre className="text-xs bg-white border rounded p-3 overflow-x-auto">
                  {JSON.stringify(detail.output.schema, null, 2)}
                </pre>
              </div>
            )}
            {detail.recorded_at && (
              <p className="text-xs text-gray-400">
                Recorded: {detail.recorded_at}
                {detail.commit && <> | Commit: <code>{detail.commit}</code></>}
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
