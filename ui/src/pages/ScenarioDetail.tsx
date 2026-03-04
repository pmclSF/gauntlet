import { useParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { getScenarios, getResults, getBaselineDiff } from '../api/client';
import { StatusBadge } from '../components/StatusBadge';
import { Loading } from '../components/Loading';

export function ScenarioDetail() {
  const { name } = useParams<{ name: string }>();

  const { data: scenarios } = useQuery({
    queryKey: ['scenarios'],
    queryFn: () => getScenarios('smoke'),
  });

  const { data: results } = useQuery({
    queryKey: ['results'],
    queryFn: getResults,
  });

  const { data: baseline } = useQuery({
    queryKey: ['baseline-diff', 'smoke', name],
    queryFn: () => getBaselineDiff('smoke', name!),
    enabled: !!name,
  });

  const scenario = scenarios?.find((s) => s.scenario === name);
  const result = results?.scenarios?.find((s) => s.name === name);

  if (!scenario && !result) return <Loading message={`Loading ${name}...`} />;

  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      <div className="flex items-center gap-2 mb-6">
        <Link to="/scenarios" className="text-blue-600 hover:underline text-sm">&larr; Scenarios</Link>
        <span className="text-gray-300">/</span>
        <h1 className="text-2xl font-bold">{name}</h1>
        {result && <StatusBadge status={result.status} />}
      </div>

      {scenario && (
        <p className="text-gray-600 mb-6">{scenario.description}</p>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 mb-6">
        {/* Panel 1: Input */}
        {scenario?.input?.messages && (
          <div className="bg-white border rounded-lg shadow-sm">
            <div className="px-4 py-3 border-b bg-gray-50 rounded-t-lg">
              <h2 className="text-sm font-semibold text-gray-500 uppercase">Input</h2>
            </div>
            <div className="p-4 space-y-3">
              {scenario.input.messages.map((msg, i) => (
                <div
                  key={i}
                  className={`p-3 rounded-lg text-sm ${
                    msg.role === 'user'
                      ? 'bg-blue-50 text-blue-900 ml-4'
                      : 'bg-gray-100 text-gray-900 mr-4'
                  }`}
                >
                  <div className="text-xs font-semibold text-gray-500 mb-1">{msg.role}</div>
                  <div className="whitespace-pre-wrap">{msg.content}</div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Panel 2: World State */}
        {scenario?.world && (
          <div className="bg-white border rounded-lg shadow-sm">
            <div className="px-4 py-3 border-b bg-gray-50 rounded-t-lg">
              <h2 className="text-sm font-semibold text-gray-500 uppercase">World State</h2>
            </div>
            <div className="p-4 space-y-4">
              {scenario.world.tools && Object.keys(scenario.world.tools).length > 0 && (
                <div>
                  <h3 className="text-xs font-semibold text-gray-500 mb-2">Tools</h3>
                  <div className="flex flex-wrap gap-2">
                    {Object.entries(scenario.world.tools).map(([tool, variant]) => (
                      <div key={tool} className="flex items-center gap-1">
                        <code className="text-xs bg-blue-50 text-blue-700 px-2 py-1 rounded">{tool}</code>
                        <span className={`text-xs px-1.5 py-0.5 rounded ${
                          variant === 'nominal' ? 'bg-green-100 text-green-700' : 'bg-yellow-100 text-yellow-700'
                        }`}>
                          {variant as string}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {scenario.world.databases && Object.keys(scenario.world.databases).length > 0 && (
                <div>
                  <h3 className="text-xs font-semibold text-gray-500 mb-2">Databases</h3>
                  <div className="flex flex-wrap gap-2">
                    {Object.entries(scenario.world.databases).map(([db, seed]) => (
                      <div key={db} className="flex items-center gap-1">
                        <code className="text-xs bg-purple-50 text-purple-700 px-2 py-1 rounded">{db}</code>
                        <span className="text-xs bg-gray-100 text-gray-600 px-1.5 py-0.5 rounded">
                          {seed as string}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </div>
        )}

        {/* Panel 3: Assertions */}
        <div className="bg-white border rounded-lg shadow-sm">
          <div className="px-4 py-3 border-b bg-gray-50 rounded-t-lg">
            <h2 className="text-sm font-semibold text-gray-500 uppercase">Assertions</h2>
          </div>
          <div className="p-4 space-y-2">
            {scenario?.assertions.map((a, i) => {
              const runAssertion = result?.assertions?.[i];
              return (
                <div key={i} className={`p-2 rounded text-sm ${
                  runAssertion ? (runAssertion.passed ? 'bg-green-50' : 'bg-red-50') : 'bg-gray-50'
                }`}>
                  <div className="flex items-center gap-2">
                    {runAssertion && (
                      <span>{runAssertion.passed ? '\u2713' : '\u2717'}</span>
                    )}
                    <code className="text-xs bg-gray-200 px-1 rounded">{a.type}</code>
                  </div>
                  {runAssertion && !runAssertion.passed && (
                    <p className="text-xs text-red-600 mt-1">{runAssertion.message}</p>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      </div>

      {/* Baseline */}
      {baseline && (
        <div className="bg-white border rounded-lg shadow-sm">
          <div className="px-4 py-3 border-b bg-gray-50 rounded-t-lg">
            <h2 className="text-sm font-semibold text-gray-500 uppercase">Baseline</h2>
          </div>
          <div className="p-4 space-y-4">
            {baseline.tool_sequence && (
              <div>
                <h3 className="text-xs font-semibold text-gray-500 mb-2">Tool Sequence</h3>
                <div className="flex gap-1 items-center flex-wrap">
                  {baseline.tool_sequence.required.map((tool, i) => (
                    <span key={i}>
                      {i > 0 && <span className="text-gray-400 mx-1">&rarr;</span>}
                      <code className="text-xs bg-blue-50 text-blue-700 px-2 py-1 rounded">{tool}</code>
                    </span>
                  ))}
                </div>
              </div>
            )}
            {baseline.output?.required_fields && baseline.output.required_fields.length > 0 && (
              <div>
                <h3 className="text-xs font-semibold text-gray-500 mb-2">Required Fields</h3>
                <div className="flex gap-1 flex-wrap">
                  {baseline.output.required_fields.map((f) => (
                    <span key={f} className="text-xs bg-green-50 text-green-700 px-2 py-0.5 rounded font-mono">{f}</span>
                  ))}
                </div>
              </div>
            )}
            {baseline.output?.forbidden_content && baseline.output.forbidden_content.length > 0 && (
              <div>
                <h3 className="text-xs font-semibold text-gray-500 mb-2">Forbidden Content</h3>
                <div className="flex gap-1 flex-wrap">
                  {baseline.output.forbidden_content.map((c) => (
                    <span key={c} className="text-xs bg-red-50 text-red-700 px-2 py-0.5 rounded">{c}</span>
                  ))}
                </div>
              </div>
            )}
            {baseline.output?.schema && (
              <div>
                <h3 className="text-xs font-semibold text-gray-500 mb-2">Output Schema</h3>
                <pre className="text-xs bg-gray-50 rounded p-3 overflow-x-auto">
                  {JSON.stringify(baseline.output.schema, null, 2)}
                </pre>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
