import { describe, it, expect, vi, beforeEach } from 'vitest';
import { getProposals, getResults, getBaselines } from './client';

const mockFetch = vi.fn();
globalThis.fetch = mockFetch;

beforeEach(() => {
  mockFetch.mockReset();
});

describe('getProposals', () => {
  it('returns parsed proposals', async () => {
    const proposals = [{ id: '1', name: 'test', status: 'pending', source: 'python_tool_ast' }];
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(proposals),
    });

    const result = await getProposals();
    expect(result).toEqual(proposals);
    expect(mockFetch).toHaveBeenCalledWith('/api/proposals', undefined);
  });

  it('throws on non-ok response', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
    });

    await expect(getProposals()).rejects.toThrow('API error: 500');
  });
});

describe('getResults', () => {
  it('returns null when no results', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ status: 'no_runs' }),
    });

    const result = await getResults();
    expect(result).toBeNull();
  });

  it('returns run result with summary', async () => {
    const data = {
      version: '1',
      suite: 'smoke',
      summary: { total: 2, passed: 2, failed: 0, skipped_budget: 0, error: 0 },
      scenarios: [],
    };
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(data),
    });

    const result = await getResults();
    expect(result).toEqual(data);
  });

  it('returns null on error', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: 'Error',
    });

    const result = await getResults();
    expect(result).toBeNull();
  });
});

describe('getBaselines', () => {
  it('returns baselines array', async () => {
    const baselines = [{ scenario: 'test1', tool_sequence: { required: ['lookup'] } }];
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(baselines),
    });

    const result = await getBaselines('smoke');
    expect(result).toEqual(baselines);
    expect(mockFetch).toHaveBeenCalledWith('/api/baselines?suite=smoke', undefined);
  });
});
