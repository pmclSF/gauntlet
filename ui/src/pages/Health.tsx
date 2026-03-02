import { useQuery } from '@tanstack/react-query';
import { getResults } from '../api/client';
import { StatusBadge } from '../components/StatusBadge';

export function Health() {
  const { data: results, isLoading, error } = useQuery({
    queryKey: ['results'],
    queryFn: getResults,
  });

  if (isLoading) return <div className="p-8 text-gray-500">Loading suite health...</div>;
  if (error) return <div className="p-8 text-red-500">Error loading results</div>;

  if (!results || !results.summary) {
    return (
      <div className="max-w-7xl mx-auto px-4 py-6">
        <h1 className="text-2xl font-bold mb-6">Suite Health</h1>
        <p className="text-gray-500">No test results available yet. Run a suite first.</p>
      </div>
    );
  }

  const { summary, scenarios } = results;
  const total = summary.passed + summary.failed + summary.skipped_budget + summary.error;
  const passRate = total > 0 ? ((summary.passed / total) * 100).toFixed(1) : '0';

  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-6">Suite Health</h1>

      {/* Summary cards */}
      <div className="grid grid-cols-4 gap-4 mb-8">
        <div className="bg-white border rounded-lg p-4 text-center">
          <div className="text-3xl font-bold text-green-600">{summary.passed}</div>
          <div className="text-sm text-gray-500">Passed</div>
        </div>
        <div className="bg-white border rounded-lg p-4 text-center">
          <div className="text-3xl font-bold text-red-600">{summary.failed}</div>
          <div className="text-sm text-gray-500">Failed</div>
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

      {/* Metadata */}
      <div className="bg-gray-50 border rounded-lg p-4 mb-6 text-sm text-gray-600">
        <span>Suite: <strong>{results.suite}</strong></span>
        <span className="mx-3">|</span>
        <span>Mode: <strong>{results.mode}</strong></span>
        <span className="mx-3">|</span>
        <span>Duration: <strong>{results.duration_ms}ms</strong> / {results.budget_ms}ms</span>
        <span className="mx-3">|</span>
        <span>Egress: <strong>{results.egress_blocked ? 'Blocked' : 'Open'}</strong></span>
      </div>

      {/* Scenario table */}
      <div className="bg-white border rounded-lg shadow-sm overflow-hidden">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
                Scenario
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
                Status
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
                Tag
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
                Duration
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
                Assertions
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200">
            {scenarios?.map((scenario) => (
              <tr key={scenario.name} className={scenario.status === 'failed' ? 'bg-red-50' : ''}>
                <td className="px-4 py-3 text-sm font-medium">{scenario.name}</td>
                <td className="px-4 py-3">
                  <StatusBadge status={scenario.status} />
                </td>
                <td className="px-4 py-3 text-sm text-gray-500">
                  {scenario.primary_tag || '-'}
                </td>
                <td className="px-4 py-3 text-sm text-gray-500">{scenario.duration_ms}ms</td>
                <td className="px-4 py-3 text-sm">
                  {scenario.assertions?.length || 0} checks
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
