import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { resetCsrfToken } from './api';
import { resetAuthMock } from './mocks';
import { resetProfileMock } from '../profile/mocks';

export function resetAuthTestState() {
  resetAuthMock();
  resetProfileMock();
  resetCsrfToken();
}

export function renderApp(
  ui: ReactNode,
  entries: Parameters<typeof MemoryRouter>[0]['initialEntries'] = ['/'],
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const result = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={entries}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...result, client };
}
