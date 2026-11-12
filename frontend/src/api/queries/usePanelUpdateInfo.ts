import { useQuery } from '@tanstack/react-query';
import i18next from 'i18next';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import type { PanelUpdateInfo } from '@/pages/index/PanelUpdateModal';

async function fetchPanelUpdateInfo(): Promise<PanelUpdateInfo> {
  const msg = await HttpUtil.get<PanelUpdateInfo>(
    '/panel/api/server/getPanelUpdateInfo',
    undefined,
    {
      silent: true,
    },
  );
  if (!msg?.success || !msg.obj) {
    throw new Error(msg?.msg || i18next.t('pages.index.panelUpdateCheckFailed'));
  }
  return msg.obj;
}

// The server caches GitHub's answer, so refetching on tab focus and every 30 min
// costs no extra GitHub calls; a release published while the page is open still shows.
export function usePanelUpdateInfo() {
  const query = useQuery({
    queryKey: keys.server.panelUpdateInfo(),
    queryFn: fetchPanelUpdateInfo,
    refetchInterval: 30 * 60_000,
    retry: false,
  });
  return {
    info: query.data,
    checkError: query.data ? undefined : query.error?.message,
    refetch: query.refetch,
  };
}
