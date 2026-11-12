import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';
import { keys } from '@/api/queryKeys';
import { DefaultsPayloadSchema, type DefaultsPayload } from '@/schemas/defaults';

async function fetchDefaults(): Promise<DefaultsPayload> {
  const msg = await HttpUtil.post('/panel/api/setting/defaultSettings', undefined, {
    silent: true,
  });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch defaults');
  return parseMsg(msg, DefaultsPayloadSchema, 'setting/defaultSettings').obj ?? {};
}

// The saved switch, re-read on every mount: the inbound form remounts per open, so a
// change saved elsewhere is never served from cache. Undefined until that read lands.
export function useSavedUsageMultiplierEnable(): boolean | undefined {
  const query = useQuery({
    queryKey: keys.settings.defaults(),
    queryFn: fetchDefaults,
    refetchOnMount: 'always',
  });
  if (!query.isFetchedAfterMount || query.isError) return undefined;
  return query.data?.usageMultiplierEnable === true;
}
