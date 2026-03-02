import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getBaselineDiff } from '../api/client';

export function Baselines() {
  const [suite, setSuite] = useState('smoke');
  const [scenario, setScenario] = useState('');

  const { data: diff, isLoading, error, refetch } = useQuery({
    queryKey: ['baseline-diff', suite, scenario],
    queryFn: () => getBaselineDiff(suite, scenario),
    enabled: false,
  });

  const handleLoad = () => {
    if (suite && scenario) {
      refetch();
    }
  };

  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-6">Baseline Diff</h1>

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
            />
          </div>
          <div className="flex-1">
            <label className="block text-sm font-medium text-gray-700 mb-1">Scenario</label>
            <input
              type="text"
              value={scenario}
              onChange={(e) => setScenario(e.target.value)}
              className="border rounded px-3 py-1.5 text-sm w-full"
              placeholder="order_status_nominal"
            />
          </div>
          <button
            onClick={handleLoad}
            disabled={!scenario}
            className="px-4 py-1.5 bg-gray-900 text-white text-sm rounded hover:bg-gray-800 disabled:opacity-50"
          >
            Load Baseline
          </button>
        </div>
      </div>

      {isLoading && <p className="text-gray-500">Loading baseline...</p>}
      {error && <p className="text-red-500">Baseline not found or error loading.</p>}

      {diff && (
        <div className="bg-white border rounded-lg shadow-sm">
          <div className="p-4 border-b bg-gray-50">
            <h2 className="font-bold">
              {diff.scenario}{' '}
              <span className="text-sm font-normal text-gray-500">({diff.suite})</span>
            </h2>
          </div>
          <div className="p-4 space-y-4">
            <div>
              <h3 className="text-sm font-semibold text-gray-500 uppercase mb-2">
                Tool Sequence
              </h3>
              <div className="flex gap-2">
                {diff.tool_sequence.map((tool, i) => (
                  <span
                    key={i}
                    className="inline-flex items-center px-2.5 py-1 rounded bg-blue-50 text-blue-700 text-sm font-mono"
                  >
                    {i + 1}. {tool}
                  </span>
                ))}
                {diff.tool_sequence.length === 0 && (
                  <span className="text-sm text-gray-400">No tool sequence defined</span>
                )}
              </div>
            </div>

            <div>
              <h3 className="text-sm font-semibold text-gray-500 uppercase mb-2">
                Required Fields
              </h3>
              <div className="flex gap-2 flex-wrap">
                {diff.required_fields.map((field) => (
                  <span
                    key={field}
                    className="inline-flex items-center px-2 py-0.5 rounded bg-green-50 text-green-700 text-xs font-mono"
                  >
                    {field}
                  </span>
                ))}
              </div>
            </div>

            {diff.forbidden_content.length > 0 && (
              <div>
                <h3 className="text-sm font-semibold text-gray-500 uppercase mb-2">
                  Forbidden Content
                </h3>
                <div className="flex gap-2 flex-wrap">
                  {diff.forbidden_content.map((content) => (
                    <span
                      key={content}
                      className="inline-flex items-center px-2 py-0.5 rounded bg-red-50 text-red-700 text-xs"
                    >
                      {content}
                    </span>
                  ))}
                </div>
              </div>
            )}

            <div>
              <h3 className="text-sm font-semibold text-gray-500 uppercase mb-2">
                Output Schema
              </h3>
              <pre className="text-xs bg-gray-50 rounded p-3 overflow-x-auto">
                {JSON.stringify(diff.output_schema, null, 2)}
              </pre>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
