import type { Proposal, IOLibrary, RunResult, BaselineDiff } from './types';

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

export async function getResults(): Promise<RunResult> {
  return fetchJSON<RunResult>('/results');
}

export async function getBaselineDiff(suite: string, scenario: string): Promise<BaselineDiff> {
  return fetchJSON<BaselineDiff>(`/baselines/diff?suite=${suite}&scenario=${scenario}`);
}
