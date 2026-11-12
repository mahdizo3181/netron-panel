import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';

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
  // The page's generic Restart Panel notice is the only restart prompt; a panel
  // restart applies the saved mode (ApplySavedUsageMultiplierMode).
  it('shows the saved and running mode, with no restart banner, while a change is pending', async () => {
    mockStatus({ configured: true, active: false, nodes: [] });
    renderSetting();
    await screen.findByText('Saved: on · Running: off');
    expect(document.querySelector('.ant-alert')).toBeNull();
    expect(screen.queryByRole('button', { name: /Restart/ })).toBeNull();
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
