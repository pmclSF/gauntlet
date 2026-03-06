import type { Proposal, IOLibrary, RunResult, BaselineContract, ScenarioDefinition } from './types';

const BASE = '/api';

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(`${BASE}${path}`, init);
  if (!resp.ok) {
    throw new Error(`API error: ${resp.status} ${resp.statusText}`);
  }
  return resp.json();
}

export async function getProposals(): Promise<Proposal[]> {
  return fetchJSON<Proposal[]>('/proposals');
}

export async function approveProposal(id: string): Promise<Proposal> {
  return fetchJSON<Proposal>('/proposals/approve', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  });
}

export async function rejectProposal(id: string): Promise<Proposal> {
  return fetchJSON<Proposal>('/proposals/reject', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  });
}

export async function getPairs(): Promise<IOLibrary[]> {
  return fetchJSON<IOLibrary[]>('/pairs');
}

export async function getResults(): Promise<RunResult | null> {
  try {
    const result = await fetchJSON<RunResult>('/results');
    if (!result || !result.summary) return null;
    return result;
  } catch {
    return null;
  }
}

export async function getRuns(): Promise<RunResult[]> {
  return fetchJSON<RunResult[]>('/runs');
}

export async function getScenarios(suite = 'smoke'): Promise<ScenarioDefinition[]> {
  return fetchJSON<ScenarioDefinition[]>(`/scenarios?suite=${suite}`);
}

export async function getBaselines(suite = 'smoke'): Promise<BaselineContract[]> {
  return fetchJSON<BaselineContract[]>(`/baselines?suite=${suite}`);
}

export async function getBaselineDiff(suite: string, scenario: string): Promise<BaselineContract> {
  return fetchJSON<BaselineContract>(`/baselines/diff?suite=${suite}&scenario=${scenario}`);
}
