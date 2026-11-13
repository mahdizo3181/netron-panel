import type { ReactNode } from 'react';
import { act, fireEvent, renderHook, screen, waitFor } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { useClients } from '@/hooks/useClients';
import FilterDrawer from '@/pages/clients/FilterDrawer';
import ClientsPage from '@/pages/clients/ClientsPage';
import { emptyFilters } from '@/pages/clients/filters';
import { HttpUtil, Msg } from '@/utils';
import { makeTestQueryClient, renderWithProviders } from './test-utils';

vi.mock('@/api/queries/useAllSettings', () => ({
  useAllSettings: () => ({ allSetting: {} }),
}));

afterEach(() => vi.restoreAllMocks());

const summary = {
  total: 2,
  active: 2,
  onlineCount: 0,
  depletedCount: 0,
  expiringCount: 0,
  deactiveCount: 0,
  online: [],
  depleted: [],
  expiring: [],
  deactive: [],
};

function mockAdmin(resellers: { id: number; name: string; username: string }[]) {
  const pagedUrls: string[] = [];
  vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
    if (url.includes('/clients/list/paged')) {
      pagedUrls.push(url);
      return new Msg(true, '', {
        items: [
          {
            email: 'shop@x',
            enable: true,
            totalGB: 0,
            expiryTime: 0,
            inboundIds: [],
            owner: { id: 3, name: 'Shop A' },
          },
          { email: 'mine@x', enable: true, totalGB: 0, expiryTime: 0, inboundIds: [], owner: null },
        ],
        total: 2,
        filtered: 2,
        page: 1,
        pageSize: 25,
        groups: [],
        summary,
      });
    }
    if (url.startsWith('/panel/api/resellers')) return new Msg(true, '', resellers);
    if (url.includes('/inbounds/options')) return new Msg(true, '', []);
    return new Msg(true, '', null);
  });
  vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string) => {
    if (url.includes('/setting/defaultSettings')) return new Msg(true, '', { pageSize: 25 });
    return new Msg(true, '', null);
  });
  return pagedUrls;
}

describe('owner filter', () => {
  it('sends owner=<id> to the paged list', async () => {
    const urls = mockAdmin([]);
    const client = makeTestQueryClient();
    const { result } = renderHook(() => useClients(), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    });
    act(() => result.current.setQuery({ page: 1, pageSize: 25, owner: '3' }));
    await waitFor(() => expect(urls.length).toBeGreaterThan(0));
    expect(new URL(urls.at(-1)!, 'http://x').searchParams.get('owner')).toBe('3');
  });

  it('offers All, the admin and each reseller once resellers exist, and nothing before', async () => {
    const onChange = vi.fn();
    const props = {
      open: true,
      onOpenChange: () => {},
      filters: emptyFilters(),
      onChange,
      inbounds: [],
      protocols: [],
      groups: [],
      nodes: [],
    };
    const view = renderWithProviders(<FilterDrawer {...props} />);
    expect(screen.queryByLabelText('Owner')).toBeNull();
    view.unmount();

    renderWithProviders(<FilterDrawer {...props} resellers={[{ id: 3, name: 'Shop A' }]} />);
    fireEvent.mouseDown(await screen.findByLabelText('Owner'));
    fireEvent.click(await screen.findByTitle('Shop A'));
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ owner: '3' }));
    expect(screen.getByTitle('My own clients')).toBeTruthy();
    expect(screen.getAllByTitle('Everyone').length).toBeGreaterThan(0);
  });
});

describe('owner badge in the admin client list', () => {
  async function renderClients() {
    renderWithProviders(
      <MemoryRouter>
        <ClientsPage />
      </MemoryRouter>,
    );
    await screen.findAllByText('shop@x');
  }

  it("tags a reseller's client with its name and the admin's own with a neutral tag", async () => {
    mockAdmin([{ id: 3, name: 'Shop A', username: 'shop-a' }]);
    await renderClients();
    await waitFor(() =>
      expect(document.querySelectorAll('.owner-tag[data-owner="3"]').length).toBeGreaterThan(0),
    );
    expect(document.querySelector('.owner-tag[data-owner="3"]')?.textContent).toBe('Shop A');
    expect(document.querySelector('.owner-tag[data-owner="admin"]')?.textContent).toBe('admin');
  });

  it('shows no owner tags on a panel without resellers', async () => {
    mockAdmin([]);
    await renderClients();
    expect(document.querySelector('.owner-tag')).toBeNull();
  });
});
