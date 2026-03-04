import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { getProposals, approveProposal, rejectProposal } from '../api/client';
import { StatusBadge } from '../components/StatusBadge';
import { EmptyState } from '../components/EmptyState';
import { Loading } from '../components/Loading';

type StatusFilter = 'all' | 'pending' | 'approved' | 'rejected';

const sourceColors: Record<string, string> = {
  python_tool_ast: 'bg-blue-100 text-blue-800',
  tool_yaml_scan: 'bg-green-100 text-green-800',
  db_schema: 'bg-purple-100 text-purple-800',
  auto_scenario: 'bg-yellow-100 text-yellow-800',
};

const frameworkColors: Record<string, string> = {
  gauntlet: 'bg-gray-200 text-gray-800',
  'pydantic-ai': 'bg-indigo-100 text-indigo-800',
  'openai-agents': 'bg-emerald-100 text-emerald-800',
  langchain: 'bg-orange-100 text-orange-800',
};

export function Proposals() {
  const queryClient = useQueryClient();
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [sourceFilter, setSourceFilter] = useState('all');

  const { data: proposals, isLoading, error } = useQuery({
    queryKey: ['proposals'],
    queryFn: getProposals,
  });

  const approveMut = useMutation({
    mutationFn: approveProposal,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['proposals'] }),
  });

  const rejectMut = useMutation({
    mutationFn: rejectProposal,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['proposals'] }),
  });

  if (isLoading) return <Loading message="Loading proposals..." />;
  if (error) return <div className="p-8 text-red-500">Error loading proposals</div>;

  if (!proposals || proposals.length === 0) {
    return (
      <div className="max-w-7xl mx-auto px-4 py-6">
        <h1 className="text-2xl font-bold mb-6">Proposals</h1>
        <EmptyState
          title="No proposals found"
          description="Run `gauntlet discover` to scan your codebase for tools and scenarios."
        />
      </div>
    );
  }

  const sources = [...new Set(proposals.map((p) => p.source))];

  const filtered = proposals.filter((p) => {
    if (statusFilter !== 'all' && p.status !== statusFilter) return false;
    if (sourceFilter !== 'all' && p.source !== sourceFilter) return false;
    return true;
  });

  const pending = filtered.filter((p) => p.status === 'pending');
  const reviewed = filtered.filter((p) => p.status !== 'pending');

  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-6">Proposals</h1>

      {/* Filter bar */}
      <div className="flex gap-4 mb-6 items-center flex-wrap">
        <div className="flex gap-1">
          {(['all', 'pending', 'approved', 'rejected'] as StatusFilter[]).map((s) => (
            <button
              key={s}
              onClick={() => setStatusFilter(s)}
              className={`px-3 py-1 text-sm rounded ${
                statusFilter === s
                  ? 'bg-gray-900 text-white'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
              }`}
            >
              {s.charAt(0).toUpperCase() + s.slice(1)}
            </button>
          ))}
        </div>
        <select
          value={sourceFilter}
          onChange={(e) => setSourceFilter(e.target.value)}
          className="border rounded px-2 py-1 text-sm"
        >
          <option value="all">All sources</option>
          {sources.map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <span className="text-sm text-gray-500">{filtered.length} proposals</span>
      </div>

      {pending.length > 0 && (
        <>
          <h2 className="text-lg font-bold mb-3">Pending ({pending.length})</h2>
          <div className="space-y-3 mb-8">
            {pending.map((proposal) => (
              <div
                key={proposal.id}
                className="bg-white border rounded-lg p-4 flex items-center justify-between shadow-sm"
              >
                <div className="flex-1">
                  <div className="flex items-center gap-2 mb-1 flex-wrap">
                    <span className="font-medium">{proposal.name}</span>
                    <StatusBadge status={proposal.status} />
                    <span className={`text-xs px-2 py-0.5 rounded ${sourceColors[proposal.source] || 'bg-gray-100 text-gray-600'}`}>
                      {proposal.source}
                    </span>
                    {proposal.framework && (
                      <span className={`text-xs px-2 py-0.5 rounded ${frameworkColors[proposal.framework] || 'bg-gray-100 text-gray-600'}`}>
                        {proposal.framework}
                      </span>
                    )}
                    {(proposal.tags || []).map((tag) => (
                      <span key={tag} className="text-xs bg-gray-100 text-gray-600 px-2 py-0.5 rounded">
                        {tag}
                      </span>
                    ))}
                  </div>
                  <p className="text-sm text-gray-600">{proposal.description}</p>
                  <p className="text-xs text-gray-400 mt-1">
                    {proposal.tool ? `Tool: ${proposal.tool}` : `Database: ${proposal.database}`}
                    {proposal.variant && ` | Variant: ${proposal.variant}`}
                    {proposal.seed_set && ` | Seed: ${proposal.seed_set}`}
                  </p>
                </div>
                <div className="flex gap-2 ml-4">
                  <button
                    onClick={() => approveMut.mutate(proposal.id)}
                    disabled={approveMut.isPending}
                    className="px-3 py-1.5 bg-green-600 text-white text-sm rounded hover:bg-green-700 disabled:opacity-50"
                  >
                    Approve
                  </button>
                  <button
                    onClick={() => rejectMut.mutate(proposal.id)}
                    disabled={rejectMut.isPending}
                    className="px-3 py-1.5 bg-red-600 text-white text-sm rounded hover:bg-red-700 disabled:opacity-50"
                  >
                    Reject
                  </button>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      {reviewed.length > 0 && (
        <>
          <h2 className="text-lg font-bold mb-3">Reviewed ({reviewed.length})</h2>
          <div className="space-y-2">
            {reviewed.map((proposal) => (
              <div
                key={proposal.id}
                className="bg-gray-50 border rounded-lg p-3 flex items-center gap-3"
              >
                <StatusBadge status={proposal.status} />
                <span className="font-medium">{proposal.name}</span>
                <span className={`text-xs px-2 py-0.5 rounded ${sourceColors[proposal.source] || 'bg-gray-100 text-gray-600'}`}>
                  {proposal.source}
                </span>
                {proposal.framework && (
                  <span className={`text-xs px-2 py-0.5 rounded ${frameworkColors[proposal.framework] || 'bg-gray-100 text-gray-600'}`}>
                    {proposal.framework}
                  </span>
                )}
                <span className="text-sm text-gray-500">{proposal.description}</span>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
