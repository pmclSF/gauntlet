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

export interface ScenarioResult {
  name: string;
  status: 'passed' | 'failed' | 'skipped' | 'error';
  duration_ms: number;
  primary_tag?: string;
  assertions: AssertionResult[];
}

export interface AssertionResult {
  type: string;
  passed: boolean;
  message: string;
  soft: boolean;
}

export interface RunResult {
  version: number;
  suite: string;
  commit: string;
  duration_ms: number;
  budget_ms: number;
  mode: string;
  egress_blocked: boolean;
  summary: {
    passed: number;
    failed: number;
    skipped_budget: number;
    error: number;
  };
  scenarios: ScenarioResult[];
}

export interface BaselineDiff {
  scenario: string;
  suite: string;
  tool_sequence: string[];
  output_schema: Record<string, unknown>;
  required_fields: string[];
  forbidden_content: string[];
}
