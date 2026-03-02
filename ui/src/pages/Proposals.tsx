import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { getProposals, approveProposal, rejectProposal } from '../api/client';
import { StatusBadge } from '../components/StatusBadge';

export function Proposals() {
  const queryClient = useQueryClient();
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

  if (isLoading) return <div className="p-8 text-gray-500">Loading proposals...</div>;
  if (error) return <div className="p-8 text-red-500">Error loading proposals</div>;

  const pending = proposals?.filter((p) => p.status === 'pending') || [];
  const reviewed = proposals?.filter((p) => p.status !== 'pending') || [];

  return (
    <div className="max-w-7xl mx-auto px-4 py-6">
      <h1 className="text-2xl font-bold mb-6">Pending Approvals</h1>

      {pending.length === 0 ? (
        <p className="text-gray-500">No pending proposals.</p>
      ) : (
        <div className="space-y-3">
          {pending.map((proposal) => (
            <div
              key={proposal.id}
              className="bg-white border rounded-lg p-4 flex items-center justify-between shadow-sm"
            >
              <div className="flex-1">
                <div className="flex items-center gap-2 mb-1">
                  <span className="font-medium">{proposal.name}</span>
                  <StatusBadge status={proposal.status} />
                  {proposal.tags.map((tag) => (
                    <span
                      key={tag}
                      className="inline-flex items-center px-2 py-0.5 rounded text-xs bg-gray-100 text-gray-600"
                    >
                      {tag}
                    </span>
                  ))}
                </div>
                <p className="text-sm text-gray-600">{proposal.description}</p>
                <p className="text-xs text-gray-400 mt-1">
                  Tool: {proposal.tool} | Variant: {proposal.variant} | Source: {proposal.source}
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
      )}

      {reviewed.length > 0 && (
        <>
          <h2 className="text-xl font-bold mt-8 mb-4">Reviewed</h2>
          <div className="space-y-2">
            {reviewed.map((proposal) => (
              <div
                key={proposal.id}
                className="bg-gray-50 border rounded-lg p-3 flex items-center gap-3"
              >
                <StatusBadge status={proposal.status} />
                <span className="font-medium">{proposal.name}</span>
                <span className="text-sm text-gray-500">{proposal.description}</span>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
