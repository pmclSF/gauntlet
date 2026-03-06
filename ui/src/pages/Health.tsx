import { useState, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getResults } from '../api/client';
import { StatusBadge } from '../components/StatusBadge';
import { EmptyState } from '../components/EmptyState';
import { Loading } from '../components/Loading';
import type { ScenarioResult } from '../api/types';

const statusOrder: Record<string, number> = { failed: 0, error: 1, passed: 2, skipped: 3 };

function sortScenarios(scenarios: ScenarioResult[]): ScenarioResult[] {
  return [...scenarios].sort(
    (a, b) => (statusOrder[a.status] ?? 9) - (statusOrder[b.status] ?? 9)
  );
}

export function Health() {
  const { data: results, isLoading, error } = useQuery({
    queryKey: ['results'],
    queryFn: getResults,
  });

  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (results?.scenarios) {
      const failedNames = new Set(
        results.scenarios
          .filter((s) => s.status === 'failed' || s.status === 'error')
          .map((s) => s.name)
      );
      setExpanded(failedNames);
    }
  }, [results]);

  const toggle = (name: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  if (isLoading) return <Loading message="Loading suite health..." />;
  if (error) return <div className="p-8 text-red-500">Error loading results</div>;

  if (!results || !results.summary) {
    return (
      <div className="max-w-7xl mx-auto px-4 py-6">
        <h1 className="text-2xl font-bold mb-6">Suite Health</h1>
        <EmptyState
          title="No test results yet"
          description="Run `gauntlet run --suite smoke` to generate results."
        />
      </div>
    );
  }

  const { summary, scenarios } = results;
  const total = summary.total || (summary.passed + summary.failed + summary.skipped_budget + summary.error);
  const passRate = total > 0 ? ((summary.passed / total) * 100).toFixed(1) : '0';
  const sorted = sortScenarios(scenarios || []);

  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-6">Suite Health</h1>

      <div className="grid grid-cols-5 gap-4 mb-8">
        <div className="bg-white border rounded-lg p-4 text-center">
          <div className="text-3xl font-bold text-green-600">{summary.passed}</div>
          <div className="text-sm text-gray-500">Passed</div>
        </div>
        <div className="bg-white border rounded-lg p-4 text-center">
          <div className="text-3xl font-bold text-red-600">{summary.failed}</div>
          <div className="text-sm text-gray-500">Failed</div>
        </div>
        <div className="bg-white border rounded-lg p-4 text-center">
          <div className="text-3xl font-bold text-orange-600">{summary.error}</div>
          <div className="text-sm text-gray-500">Errors</div>
        </div>
        <div className="bg-white border rounded-lg p-4 text-center">
          <div className="text-3xl font-bold text-yellow-600">{summary.skipped_budget}</div>
          <div className="text-sm text-gray-500">Skipped</div>
        </div>
        <div className="bg-white border rounded-lg p-4 text-center">
          <div className="text-3xl font-bold text-blue-600">{passRate}%</div>
          <div className="text-sm text-gray-500">Pass Rate</div>
        </div>
      </div>

      <div className="bg-gray-50 border rounded-lg p-4 mb-6 text-sm text-gray-600">
        <span>Suite: <strong>{results.suite}</strong></span>
        <span className="mx-3">|</span>
        <span>Mode: <strong>{results.mode}</strong></span>
        <span className="mx-3">|</span>
        <span>Duration: <strong>{results.duration_ms}ms</strong> / {results.budget_ms}ms</span>
        <span className="mx-3">|</span>
        <span>Egress: <strong>{results.egress_blocked ? 'Blocked' : 'Open'}</strong></span>
        <span className="mx-3">|</span>
        <span>Commit: <code className="text-xs bg-gray-200 px-1 rounded">{results.commit}</code></span>
      </div>

      <div className="bg-white border rounded-lg shadow-sm overflow-hidden">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Scenario</th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Tag</th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Duration</th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Assertions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200">
            {sorted.map((scenario) => {
              const isExpanded = expanded.has(scenario.name);
              const passCount = scenario.assertions?.filter((a) => a.passed).length ?? 0;
              const totalAssertions = scenario.assertions?.length ?? 0;

              return (
                <ExpandableRow
                  key={scenario.name}
                  scenario={scenario}
                  isExpanded={isExpanded}
                  onToggle={() => toggle(scenario.name)}
                  passCount={passCount}
                  totalAssertions={totalAssertions}
                />
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ExpandableRow({
  scenario,
  isExpanded,
  onToggle,
  passCount,
  totalAssertions,
}: {
  scenario: ScenarioResult;
  isExpanded: boolean;
  onToggle: () => void;
  passCount: number;
  totalAssertions: number;
}) {
  return (
    <>
      <tr
        className={`cursor-pointer hover:bg-gray-50 ${
          scenario.status === 'failed' ? 'bg-red-50' : scenario.status === 'error' ? 'bg-orange-50' : ''
        }`}
        onClick={onToggle}
      >
        <td className="px-4 py-3 text-sm font-medium">
          <span className="mr-1 text-gray-400">{isExpanded ? '\u25BC' : '\u25B6'}</span>
          {scenario.name}
        </td>
        <td className="px-4 py-3">
          <StatusBadge status={scenario.status} />
        </td>
        <td className="px-4 py-3 text-sm text-gray-500 font-mono">
          {scenario.primary_tag || '-'}
        </td>
        <td className="px-4 py-3 text-sm text-gray-500">{scenario.duration_ms}ms</td>
        <td className="px-4 py-3 text-sm">
          <span className={passCount === totalAssertions ? 'text-green-600' : 'text-red-600'}>
            {passCount}/{totalAssertions}
          </span>
        </td>
      </tr>
      {isExpanded && (
        <tr>
          <td colSpan={5} className="px-8 py-4 bg-gray-50 border-t">
            {scenario.culprit && (
              <div className="mb-4 p-3 bg-yellow-50 border border-yellow-200 rounded">
                <div className="flex items-center gap-2 mb-1">
                  <span className="text-xs font-semibold text-yellow-800 uppercase">Culprit</span>
                  <span className="text-xs bg-yellow-200 text-yellow-900 px-1.5 py-0.5 rounded">
                    {scenario.culprit.class}
                  </span>
                  <span className="text-xs text-yellow-700">
                    ({scenario.culprit.confidence})
                  </span>
                </div>
                <p className="text-sm text-yellow-900">{scenario.culprit.reasoning}</p>
              </div>
            )}

            <div className="space-y-2">
              {scenario.assertions?.map((a, i) => (
                <div
                  key={i}
                  className={`flex items-start gap-2 p-2 rounded ${
                    a.passed ? 'bg-green-50' : 'bg-red-50'
                  }`}
                >
                  <span className="mt-0.5 flex-shrink-0">{a.passed ? '\u2713' : '\u2717'}</span>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <code className="text-xs bg-gray-200 px-1 rounded">{a.type}</code>
                      {a.soft && (
                        <span className="text-xs text-gray-500">(soft)</span>
                      )}
                    </div>
                    <p className="text-sm text-gray-700 mt-0.5">{a.message}</p>
                    {!a.passed && a.expected && (
                      <div className="mt-1 text-xs font-mono">
                        <div className="text-green-700">Expected: {a.expected}</div>
                        <div className="text-red-700">Actual: {a.actual}</div>
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>

            {scenario.docket_tags && scenario.docket_tags.length > 0 && (
              <div className="mt-3 flex gap-1">
                {scenario.docket_tags.map((tag) => (
                  <span key={tag} className="text-xs bg-purple-100 text-purple-800 px-2 py-0.5 rounded">
                    {tag}
                  </span>
                ))}
              </div>
            )}
          </td>
        </tr>
      )}
    </>
  );
}
