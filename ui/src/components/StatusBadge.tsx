interface StatusBadgeProps {
  status: string;
}

const colors: Record<string, string> = {
  passed: 'bg-green-100 text-green-800',
  failed: 'bg-red-100 text-red-800',
  skipped: 'bg-yellow-100 text-yellow-800',
  error: 'bg-orange-100 text-orange-800',
  pending: 'bg-gray-100 text-gray-800',
  approved: 'bg-blue-100 text-blue-800',
  rejected: 'bg-red-100 text-red-600',
};

export function StatusBadge({ status }: StatusBadgeProps) {
  const color = colors[status] || 'bg-gray-100 text-gray-800';
  return (
    <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${color}`}>
      {status}
    </span>
  );
}
