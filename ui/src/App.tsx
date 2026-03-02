import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Navbar } from './components/Navbar';
import { Proposals } from './pages/Proposals';
import { Pairs } from './pages/Pairs';
import { Health } from './pages/Health';
import { Baselines } from './pages/Baselines';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 30_000,
    },
  },
});

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <div className="min-h-screen bg-gray-100">
          <Navbar />
          <Routes>
            <Route path="/" element={<Proposals />} />
            <Route path="/pairs" element={<Pairs />} />
            <Route path="/health" element={<Health />} />
            <Route path="/baselines" element={<Baselines />} />
          </Routes>
        </div>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

export default App;
