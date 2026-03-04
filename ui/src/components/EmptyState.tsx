interface EmptyStateProps {
  title: string;
  description: string;
  action?: {
    label: string;
    href?: string;
  };
}

export function EmptyState({ title, description, action }: EmptyStateProps) {
  return (
    <div className="text-center py-12">
      <h3 className="text-lg font-medium text-gray-900 mb-2">{title}</h3>
      <p className="text-sm text-gray-500 mb-4 max-w-md mx-auto font-mono">{description}</p>
      {action && action.href && (
        <a
          href={action.href}
          className="text-sm text-blue-600 hover:text-blue-800 underline"
        >
          {action.label}
        </a>
      )}
    </div>
  );
}
