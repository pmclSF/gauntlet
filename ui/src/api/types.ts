export interface Proposal {
  id: string;
  name: string;
  description: string;
  tool?: string;
  variant?: string;
  database?: string;
  seed_set?: string;
  tags?: string[];
  status: 'pending' | 'approved' | 'rejected';
  source: string;
  framework?: string;
}

export interface IOPair {
  id: string;
  description: string;
  input: Record<string, unknown>;
  output: Record<string, unknown>;
  category: 'good' | 'bad' | 'edge';
  tags: string[];
}

export interface IOLibrary {
  name: string;
  tool?: string;
  pairs: IOPair[];
}

export interface Culprit {
  class: string;
  confidence: string;
  reasoning: string;
}

export interface AssertionResult {
  type: string;
  passed: boolean;
  message: string;
  soft: boolean;
  expected?: string;
  actual?: string;
  docket_hint?: string;
}

export interface ScenarioResult {
  name: string;
  status: 'passed' | 'failed' | 'skipped' | 'error';
  duration_ms: number;
  primary_tag?: string;
  assertions: AssertionResult[];
  culprit?: Culprit;
  docket_tags?: string[];
}

export interface RunResult {
  version: string;
  suite: string;
  commit: string;
  duration_ms: number;
  budget_ms: number;
  mode: string;
  egress_blocked: boolean;
  started_at?: string;
  budget_remaining_ms?: number;
  summary: {
    total: number;
    passed: number;
    failed: number;
    skipped_budget: number;
    error: number;
  };
  scenarios: ScenarioResult[];
}

export interface BaselineContract {
  baseline_type?: string;
  scenario: string;
  suite?: string;
  recorded_at?: string;
  commit?: string;
  tool_sequence?: {
    required: string[];
    order?: string;
  };
  output?: {
    schema?: Record<string, unknown>;
    required_fields?: string[];
    forbidden_content?: string[];
  };
}

export interface ScenarioDefinition {
  scenario: string;
  description: string;
  tags?: string[];
  beta_model?: boolean;
  beta_reason?: string;
  input: {
    messages?: Array<{ role: string; content: string }>;
  };
  world: {
    tools?: Record<string, string>;
    databases?: Record<string, string>;
  };
  assertions: Array<{
    type: string;
    [key: string]: unknown;
  }>;
}

// Keep backward compat alias
export type BaselineDiff = BaselineContract;
