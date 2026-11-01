import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import UsageMultiplierSetting from '@/pages/settings/UsageMultiplierSetting';
import { AllSetting } from '@/models/setting';
import type { UsageMultiplierStatus } from '@/generated/types';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';

afterEach(() => vi.restoreAllMocks());

function mockStatus(status: UsageMultiplierStatus) {
  return vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string) => {
    if (url.endsWith('/setting/usageMultiplierStatus')) return new Msg(true, '', status);
    return new Msg(true, '', null);
  });
}

function renderSetting() {
  const setting = new AllSetting();
  renderWithProviders(<UsageMultiplierSetting allSetting={setting} updateSetting={() => {}} />);
}

describe('UsageMultiplierSetting', () => {
  it('offers the forced restart, with the drop warning, while the saved mode is not live', async () => {
    const post = mockStatus({ configured: true, active: false, nodes: [] });
    renderSetting();
    await screen.findByText('Pending: restart Xray to apply');
    expect(screen.getByText('Saved: on · Running: off')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Restart Xray/ }));
    expect(
      await screen.findByText('Restart Xray now? All connections will drop once.'),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'OK' }));
    await waitFor(() => expect(post).toHaveBeenCalledWith('/panel/api/server/restartXrayService'));
  });

  it('shows no pending prompt once the running mode matches the saved one', async () => {
    mockStatus({ configured: true, active: true, nodes: [] });
    renderSetting();
    await screen.findByText('Saved: on · Running: on');
    expect(screen.queryByText('Pending: restart Xray to apply')).toBeNull();
  });

  it('warns about nodes that would count weighted inbounds at x1', async () => {
    mockStatus({
      configured: true,
      active: true,
      nodes: [
        { nodeId: 1, name: 'old-node', known: true, applies: false, inboundIds: [5, 7] },
        { nodeId: 2, name: 'good-node', known: true, applies: true, inboundIds: [9] },
      ],
    });
    renderSetting();
    expect(await screen.findByText(/old-node \(inbounds #5, #7\)/)).toBeTruthy();
    expect(screen.queryByText(/good-node/)).toBeNull();
  });
});
