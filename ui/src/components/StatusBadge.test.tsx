import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { StatusBadge } from './StatusBadge';

describe('StatusBadge', () => {
  it('renders the status text', () => {
    render(<StatusBadge status="passed" />);
    expect(screen.getByText('passed')).toBeInTheDocument();
  });

  it('applies green color for passed', () => {
    render(<StatusBadge status="passed" />);
    const badge = screen.getByText('passed');
    expect(badge.className).toContain('text-green');
  });

  it('applies red color for failed', () => {
    render(<StatusBadge status="failed" />);
    const badge = screen.getByText('failed');
    expect(badge.className).toContain('text-red');
  });

  it('applies yellow color for skipped', () => {
    render(<StatusBadge status="skipped" />);
    const badge = screen.getByText('skipped');
    expect(badge.className).toContain('text-yellow');
  });

  it('applies orange color for error', () => {
    render(<StatusBadge status="error" />);
    const badge = screen.getByText('error');
    expect(badge.className).toContain('text-orange');
  });

  it('falls back to gray for unknown status', () => {
    render(<StatusBadge status="unknown" />);
    const badge = screen.getByText('unknown');
    expect(badge.className).toContain('text-gray');
  });
});
