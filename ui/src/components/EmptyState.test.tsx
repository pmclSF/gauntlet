import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { EmptyState } from './EmptyState';

describe('EmptyState', () => {
  it('renders title and description', () => {
    render(<EmptyState title="No data" description="Try again later." />);
    expect(screen.getByText('No data')).toBeInTheDocument();
    expect(screen.getByText('Try again later.')).toBeInTheDocument();
  });

  it('renders action link when provided', () => {
    render(
      <EmptyState
        title="No data"
        description="Try again."
        action={{ label: 'Go home', href: '/' }}
      />
    );
    const link = screen.getByText('Go home');
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute('href', '/');
  });

  it('does not render action link when not provided', () => {
    render(<EmptyState title="Empty" description="Nothing here." />);
    expect(screen.queryByRole('link')).toBeNull();
  });
});
