import { useQuery } from '@tanstack/react-query';
import { getPairs } from '../api/client';
import { StatusBadge } from '../components/StatusBadge';
import { EmptyState } from '../components/EmptyState';
import { Loading } from '../components/Loading';

export function Pairs() {
  const { data: libraries, isLoading, error } = useQuery({
    queryKey: ['pairs'],
    queryFn: getPairs,
  });

  if (isLoading) return <Loading message="Loading IO pairs..." />;
  if (error) return <div className="p-8 text-red-500">Error loading IO pairs</div>;

  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-6">IO Pair Libraries</h1>

      {!libraries || libraries.length === 0 ? (
        <EmptyState
          title="No IO pair libraries found"
          description="Create YAML files in `evals/pairs/` to define input/output test pairs."
        />
      ) : (
        <div className="space-y-6">
          {libraries.map((lib) => (
            <div key={lib.name} className="bg-white border rounded-lg shadow-sm">
              <div className="p-4 border-b bg-gray-50 rounded-t-lg">
                <h2 className="font-bold text-lg">{lib.name}</h2>
                {lib.tool && (
                  <span className="text-sm text-gray-500">Tool: {lib.tool}</span>
                )}
                <span className="ml-4 text-sm text-gray-400">
                  {lib.pairs.length} pair{lib.pairs.length !== 1 ? 's' : ''}
                </span>
              </div>
              <div className="divide-y">
                {lib.pairs.map((pair) => (
                  <div key={pair.id} className="p-4">
                    <div className="flex items-center gap-2 mb-2">
                      <span className="font-medium text-sm">{pair.id}</span>
                      <StatusBadge status={pair.category} />
                      {pair.tags.map((tag) => (
                        <span
                          key={tag}
                          className="text-xs bg-gray-100 text-gray-600 px-2 py-0.5 rounded"
                        >
                          {tag}
                        </span>
                      ))}
                    </div>
                    <p className="text-sm text-gray-600 mb-3">{pair.description}</p>
                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <h4 className="text-xs font-semibold text-gray-500 uppercase mb-1">
                          Input
                        </h4>
                        <pre className="text-xs bg-gray-50 rounded p-2 overflow-x-auto">
                          {JSON.stringify(pair.input, null, 2)}
                        </pre>
                      </div>
                      <div>
                        <h4 className="text-xs font-semibold text-gray-500 uppercase mb-1">
                          Expected Output
                        </h4>
                        <pre className="text-xs bg-gray-50 rounded p-2 overflow-x-auto">
                          {JSON.stringify(pair.output, null, 2)}
                        </pre>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
