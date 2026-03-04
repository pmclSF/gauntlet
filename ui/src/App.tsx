import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Navbar } from './components/Navbar';
import { ErrorBoundary } from './components/ErrorBoundary';
import { Proposals } from './pages/Proposals';
import { Pairs } from './pages/Pairs';
import { Health } from './pages/Health';
import { Baselines } from './pages/Baselines';
import { Scenarios } from './pages/Scenarios';
import { ScenarioDetail } from './pages/ScenarioDetail';

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
          <ErrorBoundary>
            <Routes>
              <Route path="/" element={<Proposals />} />
              <Route path="/pairs" element={<Pairs />} />
              <Route path="/health" element={<Health />} />
              <Route path="/baselines" element={<Baselines />} />
              <Route path="/scenarios" element={<Scenarios />} />
              <Route path="/scenarios/:name" element={<ScenarioDetail />} />
            </Routes>
          </ErrorBoundary>
        </div>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

export default App;
