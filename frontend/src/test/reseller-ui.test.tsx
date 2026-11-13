import { act, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, RouterProvider, createMemoryRouter } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import AppSidebar from '@/layouts/AppSidebar';
import { buildRoutes } from '@/routes';
import { PANEL_ROUTE_PATHS, RESELLER_ROUTES } from '@/routes/panelRoutes';
import ResellerHome from '@/pages/reseller-home/ResellerHome';
import ResellerClientsPage from '@/pages/reseller-clients/ResellerClientsPage';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';

vi.mock('@/api/queries/useAllSettings', () => ({
  useAllSettings: () => ({ allSetting: {} }),
}));

const GB = 1024 ** 3;

function me(over: Record<string, unknown> = {}) {
  return {
    role: 'reseller',
    reseller: {
      username: 'shop-a',
      name: 'Shop A',
      poolBytes: 1000 * GB,
      usedTraffic: 600 * GB,
      remainingBytes: 400 * GB,
      maxClients: 0,
      clientCount: 2,
      expiryTime: 0,
      requireClientQuota: false,
      state: 'ok',
      twoFactorEnabled: false,
      ...over,
    },
  };
}

function page(items: Record<string, unknown>[]) {
  return { items, total: items.length, filtered: items.length, page: 1, pageSize: 25, groups: [] };
}

const getUrls: string[] = [];

function mockApi(account: ReturnType<typeof me>, rows: Record<string, unknown>[] = []) {
  getUrls.length = 0;
  vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
    getUrls.push(url);
    if (url.startsWith('/panel/api/reseller/me')) return new Msg(true, '', account);
    if (url.startsWith('/panel/api/clients/list/paged')) return new Msg(true, '', page(rows));
    if (url.startsWith('/panel/api/inbounds/options'))
      return new Msg(true, '', [{ id: 1, remark: 'edge', protocol: 'vless', port: 443 }]);
    return new Msg(true, '', null);
  });
  vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string) => {
    if (url.includes('defaultSettings'))
      return new Msg(true, '', { subEnable: true, subURI: 'https://sub.example/s/' });
    return new Msg(true, '', null);
  });
}

beforeEach(() => {
  window.X_UI_ROLE = 'reseller';
});

afterEach(() => {
  delete window.X_UI_ROLE;
  vi.restoreAllMocks();
});

describe('reseller navigation', () => {
  it('routes only Home and Clients, and sends every other path home', () => {
    expect(RESELLER_ROUTES.map((r) => r.path)).toEqual(['/', '/clients']);
    const children = buildRoutes(true)[0].children ?? [];
    expect(children.map((c) => (c.index ? '/' : `/${c.path}`))).toEqual(['/', '/clients', '/*']);
    const admin = (buildRoutes(false)[0].children ?? []).map((c) => (c.index ? '/' : `/${c.path}`));
    expect(admin.sort()).toEqual([...PANEL_ROUTE_PATHS].sort());
  });

  it('shows only Home and Clients in the sidebar, and no command palette', async () => {
    renderWithProviders(
      <MemoryRouter>
        <AppSidebar />
      </MemoryRouter>,
    );
    await act(async () => {});
    const labels = screen.getAllByRole('menuitem').map((item) => item.textContent?.trim());
    expect(labels).toEqual(['Overview', 'Clients', 'Log Out']);
    expect(screen.queryByRole('button', { name: /Command Palette/ })).toBeNull();
  });

  it.each([
    '/settings',
    '/nodes',
    '/xray',
    '/resellers',
    '/inbounds',
    '/groups',
    '/api-docs',
    '/no-such-page',
  ])('redirects a reseller from %s to Home', async (path) => {
    mockApi(me());
    const router = createMemoryRouter(buildRoutes(true), { initialEntries: [path] });
    renderWithProviders(<RouterProvider router={router} />);
    await waitFor(() => expect(router.state.location.pathname).toBe('/'));
    expect(await screen.findByText('Shop A')).toBeTruthy();
  });
});

describe('ResellerHome', () => {
  it('shows total, used, remaining and the percentage from the pool counter', async () => {
    mockApi(me());
    renderWithProviders(
      <MemoryRouter>
        <ResellerHome />
      </MemoryRouter>,
    );
    expect(await screen.findByText('60%')).toBeTruthy();
    expect(screen.getByText('1000.00 GB')).toBeTruthy();
    expect(screen.getByText('600.00 GB')).toBeTruthy();
    expect(screen.getByText('400.00 GB')).toBeTruthy();
    expect(screen.getByTestId('client-count').textContent).toBe('2');
  });

  // The pool counter only drops by an admin action; a client reset must not move the
  // home numbers, so the home never sums the client list.
  it('keeps the numbers when the client list shows zero usage after a reset', async () => {
    mockApi(me(), [
      {
        email: 'a@x',
        enable: true,
        totalGB: 0,
        expiryTime: 0,
        inboundIds: [1],
        traffic: { up: 0, down: 0 },
      },
    ]);
    renderWithProviders(
      <MemoryRouter>
        <ResellerHome />
      </MemoryRouter>,
    );
    expect(await screen.findByText('60%')).toBeTruthy();
    expect(screen.getByText('600.00 GB')).toBeTruthy();
    expect(getUrls.some((u) => u.includes('/clients/list'))).toBe(false);
  });

  it.each([
    ['pool', 'Volume used up'],
    ['expired', 'Expired'],
    ['disabled', 'Disabled'],
  ])('explains the %s state', async (state, title) => {
    mockApi(me({ state }));
    renderWithProviders(
      <MemoryRouter>
        <ResellerHome />
      </MemoryRouter>,
    );
    const alert = (await screen.findByText(title)).closest('.ant-alert') as HTMLElement;
    expect(alert).toBeTruthy();
    expect(
      within(alert).getByText(/Adding or enabling clients is blocked|disabled by the admin/),
    ).toBeTruthy();
  });

  it('shows the client limit and the expiry when set', async () => {
    mockApi(me({ maxClients: 5, clientCount: 3, expiryTime: Date.UTC(2030, 0, 2) }));
    renderWithProviders(
      <MemoryRouter>
        <ResellerHome />
      </MemoryRouter>,
    );
    expect(await screen.findByTestId('client-count')).toHaveProperty('textContent', '3 / 5');
    expect(screen.queryByText('Never')).toBeNull();
  });
});

describe('ResellerClientsPage', () => {
  const rows = [
    {
      email: 'on@x',
      enable: true,
      totalGB: 0,
      expiryTime: 0,
      inboundIds: [1],
      subId: 's1',
      owner: { id: 9, name: 'Leaked' },
    },
    { email: 'off@x', enable: false, totalGB: 0, expiryTime: 0, inboundIds: [1], subId: 's2' },
  ];

  async function renderPage(account: ReturnType<typeof me>) {
    mockApi(account, rows);
    renderWithProviders(
      <MemoryRouter>
        <ResellerClientsPage />
      </MemoryRouter>,
    );
    await screen.findByText('on@x');
  }

  it.each([
    ['pool', {}, 'Volume used up'],
    ['expired', {}, 'Expired'],
    ['disabled', {}, 'Disabled'],
    ['ok', { maxClients: 2, clientCount: 2 }, 'Client limit reached'],
  ])('disables Add with the reason while %s', async (state, extra, reason) => {
    await renderPage(me({ state, ...extra }));
    const add = screen.getByRole('button', { name: /Add Client/i });
    await waitFor(() => expect((add as HTMLButtonElement).disabled).toBe(true));
    expect(within(screen.getByTestId('add-blocked')).getByText(reason)).toBeTruthy();
  });

  it('blocks enabling a client while cut off but still allows disabling', async () => {
    await renderPage(me({ state: 'pool' }));
    const enableOff = screen.getByRole('switch', { name: /off@x/ });
    const enableOn = screen.getByRole('switch', { name: /on@x/ });
    await waitFor(() => expect((enableOff as HTMLButtonElement).disabled).toBe(true));
    expect((enableOn as HTMLButtonElement).disabled).toBe(false);
  });

  it('lets an active reseller add and enable', async () => {
    await renderPage(me());
    await waitFor(() =>
      expect(
        (screen.getByRole('button', { name: /Add Client/i }) as HTMLButtonElement).disabled,
      ).toBe(false),
    );
    expect((screen.getByRole('switch', { name: /off@x/ }) as HTMLButtonElement).disabled).toBe(
      false,
    );
    expect(screen.queryByTestId('add-blocked')).toBeNull();
  });

  it('never shows an owner tag, even if a row carried one', async () => {
    await renderPage(me());
    expect(document.querySelector('.owner-tag')).toBeNull();
    expect(screen.queryByText('Leaked')).toBeNull();
  });
});
