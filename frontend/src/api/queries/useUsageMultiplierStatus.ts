import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import type { UsageMultiplierStatus } from '@/generated/types';

async function fetchStatus(): Promise<UsageMultiplierStatus> {
  const msg = await HttpUtil.post<UsageMultiplierStatus>(
    '/panel/api/setting/usageMultiplierStatus',
    undefined,
    { silent: true },
  );
  if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Failed to fetch multiplier status');
  return msg.obj;
}

// Polled: a node's capability is learned from its sync responses, and the saved
// switch changes when the settings are saved.
export function useUsageMultiplierStatus() {
  return useQuery({
    queryKey: keys.settings.usageMultiplierStatus(),
    queryFn: fetchStatus,
    refetchInterval: 5_000,
  });
}
