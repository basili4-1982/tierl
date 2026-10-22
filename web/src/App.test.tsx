import { render, screen } from '@testing-library/react';
import { App } from './App';

it('mounts the app with providers', () => {
  render(<App />);
  expect(screen.getByRole('heading', { name: 'Tierl' })).toBeInTheDocument();
});
