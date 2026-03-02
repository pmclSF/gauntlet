import { Link, useLocation } from 'react-router-dom';

const navItems = [
  { path: '/', label: 'Proposals' },
  { path: '/pairs', label: 'IO Pairs' },
  { path: '/health', label: 'Suite Health' },
  { path: '/baselines', label: 'Baselines' },
];

export function Navbar() {
  const location = useLocation();

  return (
    <nav className="bg-gray-900 text-white">
      <div className="max-w-7xl mx-auto px-4">
        <div className="flex items-center h-14">
          <Link to="/" className="font-bold text-lg mr-8">
            Gauntlet
          </Link>
          <div className="flex space-x-1">
            {navItems.map((item) => (
              <Link
                key={item.path}
                to={item.path}
                className={`px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                  location.pathname === item.path
                    ? 'bg-gray-700 text-white'
                    : 'text-gray-300 hover:bg-gray-700 hover:text-white'
                }`}
              >
                {item.label}
              </Link>
            ))}
          </div>
        </div>
      </div>
    </nav>
  );
}
