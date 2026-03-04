import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getScenarios, getResults } from '../api/client';
import { StatusBadge } from '../components/StatusBadge';
import { EmptyState } from '../components/EmptyState';
import { Loading } from '../components/Loading';

export function Scenarios() {
  const { data: scenarios, isLoading } = useQuery({
    queryKey: ['scenarios'],
    queryFn: () => getScenarios('smoke'),
  });

  const { data: results } = useQuery({
    queryKey: ['results'],
    queryFn: getResults,
  });

  if (isLoading) return <Loading message="Loading scenarios..." />;

  const resultsByName = new Map(
    results?.scenarios?.map((s) => [s.name, s]) ?? []
  );

  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-6">Scenarios</h1>

      {!scenarios || scenarios.length === 0 ? (
        <EmptyState
          title="No scenarios found"
          description="Create YAML files in `evals/smoke/` to define test scenarios."
        />
      ) : (
        <div className="bg-white border rounded-lg shadow-sm overflow-hidden">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Description</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Tags</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Assertions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {scenarios.map((s) => {
                const result = resultsByName.get(s.scenario);
                return (
                  <tr key={s.scenario} className="hover:bg-gray-50">
                    <td className="px-4 py-3 text-sm font-medium">
                      <Link
                        to={`/scenarios/${s.scenario}`}
                        className="text-blue-600 hover:underline"
                      >
                        {s.scenario}
                      </Link>
                    </td>
                    <td className="px-4 py-3 text-sm text-gray-500 truncate max-w-xs">
                      {s.description}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex gap-1 flex-wrap">
                        {s.tags?.map((tag) => (
                          <span key={tag} className="text-xs bg-gray-100 text-gray-600 px-2 py-0.5 rounded">
                            {tag}
                          </span>
                        ))}
                        {s.beta_model && (
                          <span className="text-xs bg-yellow-100 text-yellow-800 px-2 py-0.5 rounded">beta</span>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      {result ? <StatusBadge status={result.status} /> : <span className="text-xs text-gray-400">-</span>}
                    </td>
                    <td className="px-4 py-3 text-sm text-gray-500">
                      {s.assertions.length} check{s.assertions.length !== 1 ? 's' : ''}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
