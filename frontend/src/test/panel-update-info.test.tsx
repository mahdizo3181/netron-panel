import { act, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { usePanelUpdateInfo } from '@/api/queries/usePanelUpdateInfo';
import { Status } from '@/models/status';
import OverviewActionBar from '@/pages/index/OverviewActionBar';
import PanelUpdateModal from '@/pages/index/PanelUpdateModal';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';

const noop = () => {};
const checkFailed =
  'Could not check for updates (GitHub API returned status 403: API rate limit exceeded)';

function mockUpdateInfo(...replies: Msg<unknown>[]) {
  const get = vi.spyOn(HttpUtil, 'get');
  for (const reply of replies) get.mockResolvedValueOnce(reply);
  return get;
}

function info(latestVersion: string, updateAvailable: boolean) {
  return new Msg(true, '', {
    channel: 'stable',
    currentVersion: '3.11.0-netron.5',
    latestVersion,
    updateAvailable,
  });
}

function Probe() {
  const { info, checkError } = usePanelUpdateInfo();
  if (checkError) return <span>error: {checkError}</span>;
  if (!info) return <span>loading</span>;
  return <span>{info.updateAvailable ? `update: ${info.latestVersion}` : 'up to date'}</span>;
}

afterEach(() => vi.restoreAllMocks());

describe('usePanelUpdateInfo', () => {
  it('checks again when the tab becomes visible, so a new release shows without a reload', async () => {
    const get = mockUpdateInfo(info('v3.11.0-netron.5', false), info('v3.11.0-netron.6', true));
    renderWithProviders(<Probe />);
    await screen.findByText('up to date');

    await act(async () => {
      window.dispatchEvent(new Event('visibilitychange'));
    });
    expect(await screen.findByText('update: v3.11.0-netron.6')).toBeTruthy();
    expect(get).toHaveBeenCalledWith('/panel/api/server/getPanelUpdateInfo', undefined, {
      silent: true,
    });
  });

  it("reports the server's could-not-check message instead of staying silent", async () => {
    mockUpdateInfo(new Msg(false, checkFailed));
    renderWithProviders(<Probe />);
    expect(await screen.findByText(`error: ${checkFailed}`)).toBeTruthy();
  });
});

describe('panel update UI when the check failed', () => {
  it('marks the version button with the failure', () => {
    renderWithProviders(
      <OverviewActionBar
        status={new Status()}
        isMobile={false}
        accessLogEnable={false}
        panelVersion="v3.11.0-netron.5"
        latestVersion=""
        updateAvailable={false}
        updateCheckError={checkFailed}
        onStopXray={noop}
        onRestartXray={noop}
        onOpenLogs={noop}
        onOpenXrayLogs={noop}
        onOpenAmneziaWGLogs={noop}
        onOpenConfig={noop}
        onOpenBackup={noop}
        onOpenSystemHistory={noop}
        onOpenXrayMetrics={noop}
        onOpenPanelUpdate={noop}
        onOpenVersionSwitch={noop}
      />,
    );
    expect(screen.getByLabelText(checkFailed)).toBeTruthy();
  });

  it('says the check failed rather than "up to date", and offers no dev channel', () => {
    renderWithProviders(
      <PanelUpdateModal
        open
        info={{ currentVersion: '3.11.0-netron.5', latestVersion: '', updateAvailable: false }}
        checkError={checkFailed}
        onClose={noop}
        onBusy={noop}
      />,
    );
    expect(screen.getByText(checkFailed)).toBeTruthy();
    expect(screen.queryByText('Panel is up to date')).toBeNull();
    expect(screen.queryByRole('switch', { name: /dev channel/i })).toBeNull();
  });
});
