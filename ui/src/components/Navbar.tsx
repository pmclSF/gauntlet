import { Link, useLocation } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { getProposals } from '../api/client';

const navItems = [
  { path: '/', label: 'Proposals' },
  { path: '/scenarios', label: 'Scenarios' },
  { path: '/health', label: 'Suite Health' },
  { path: '/baselines', label: 'Baselines' },
  { path: '/pairs', label: 'IO Pairs' },
];

export function Navbar() {
  const location = useLocation();
  const { data: proposals } = useQuery({
    queryKey: ['proposals'],
    queryFn: getProposals,
    staleTime: 60_000,
  });

  const pendingCount = proposals?.filter((p) => p.status === 'pending').length ?? 0;

  return (
    <nav className="bg-gray-900 text-white">
      <div className="max-w-7xl mx-auto px-4">
        <div className="flex items-center h-14">
          <Link to="/" className="font-bold text-lg mr-8">
            Gauntlet
          </Link>
          <div className="flex space-x-1">
            {navItems.map((item) => {
              const isActive = item.path === '/'
                ? location.pathname === '/'
                : location.pathname.startsWith(item.path);

              return (
                <Link
                  key={item.path}
                  to={item.path}
                  className={`px-3 py-2 rounded-md text-sm font-medium transition-colors relative ${
                    isActive
                      ? 'bg-gray-700 text-white'
                      : 'text-gray-300 hover:bg-gray-700 hover:text-white'
                  }`}
                >
                  {item.label}
                  {item.path === '/' && pendingCount > 0 && (
                    <span className="ml-1.5 inline-flex items-center justify-center w-5 h-5 text-xs font-bold bg-red-500 text-white rounded-full">
                      {pendingCount}
                    </span>
                  )}
                </Link>
              );
            })}
          </div>
        </div>
      </div>
    </nav>
  );
}
