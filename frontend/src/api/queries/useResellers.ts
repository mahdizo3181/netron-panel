import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import type { ResellerMe, ResellerView } from '@/generated/types';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

// The pool numbers are the counter the server keeps, polled because a reseller
// has no WebSocket; never a sum of client usage.
export const RESELLER_ME_POLL_MS = 15_000;

export function useResellerMe(enabled = true) {
  return useQuery({
    queryKey: keys.resellers.me(),
    enabled,
    refetchInterval: RESELLER_ME_POLL_MS,
    queryFn: async (): Promise<ResellerMe> => {
      const msg = await HttpUtil.get<ResellerMe>('/panel/api/reseller/me', undefined, {
        silent: true,
      });
      if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Failed to load the account');
      return msg.obj;
    },
  });
}

export function useResellerList(enabled = true) {
  return useQuery({
    queryKey: keys.resellers.list(),
    enabled,
    queryFn: async (): Promise<ResellerView[]> => {
      const msg = await HttpUtil.get<ResellerView[]>('/panel/api/resellers', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to load resellers');
      return Array.isArray(msg.obj) ? msg.obj : [];
    },
  });
}

export interface ResellerFormPayload {
  username?: string;
  password?: string;
  name: string;
  enable: boolean;
  poolBytes: number;
  maxClients: number;
  expiryTime: number;
  requireClientQuota: boolean;
  inboundIds: number[];
}

export function useResellerMutations() {
  const queryClient = useQueryClient();
  const done = (msg: { success?: boolean } | undefined) => {
    if (msg?.success) void queryClient.invalidateQueries({ queryKey: keys.resellers.root() });
    return msg;
  };
  const create = useMutation({
    mutationFn: async (p: ResellerFormPayload) =>
      done(await HttpUtil.post('/panel/api/resellers', p, JSON_HEADERS)),
  });
  const update = useMutation({
    mutationFn: async ({ id, p }: { id: number; p: ResellerFormPayload }) =>
      done(await HttpUtil.post(`/panel/api/resellers/${id}`, p, JSON_HEADERS)),
  });
  const setPassword = useMutation({
    mutationFn: async ({ id, password }: { id: number; password: string }) =>
      done(await HttpUtil.post(`/panel/api/resellers/${id}/password`, { password }, JSON_HEADERS)),
  });
  const remove = useMutation({
    mutationFn: async ({ id, clients }: { id: number; clients: 'transfer' | 'delete' }) =>
      done(await HttpUtil.post(`/panel/api/resellers/${id}/del`, { clients }, JSON_HEADERS)),
  });
  const resetTotp = useMutation({
    mutationFn: async (id: number) =>
      done(await HttpUtil.post(`/panel/api/resellers/${id}/resetTotp`)),
  });
  return { create, update, setPassword, remove, resetTotp };
}

export interface ResellerSubSettings {
  subEnable?: boolean;
  subURI?: string;
  subTitle?: string;
  datepicker?: 'gregorian' | 'jalalian';
}

// The server returns only the subscription and display keys to a reseller.
export function useResellerSubSettings(enabled = true) {
  return useQuery({
    queryKey: [...keys.resellers.me(), 'settings'],
    enabled,
    staleTime: 5 * 60_000,
    queryFn: async (): Promise<ResellerSubSettings> => {
      const msg = await HttpUtil.post<ResellerSubSettings>(
        '/panel/api/setting/defaultSettings',
        undefined,
        {
          silent: true,
        },
      );
      return (msg?.success && msg.obj) || {};
    },
  });
}

export function useResellerSelf() {
  const queryClient = useQueryClient();
  const refresh = (msg: { success?: boolean } | undefined) => {
    if (msg?.success) void queryClient.invalidateQueries({ queryKey: keys.resellers.me() });
    return msg;
  };
  const changePassword = useMutation({
    mutationFn: async (p: { currentPassword: string; newPassword: string }) =>
      refresh(await HttpUtil.post('/panel/api/reseller/me/password', p)),
  });
  const enableTotp = useMutation({
    mutationFn: async (p: { secret: string; code: string }) =>
      refresh(await HttpUtil.post('/panel/api/reseller/me/totp', p)),
  });
  const disableTotp = useMutation({
    mutationFn: async (code: string) =>
      refresh(await HttpUtil.post('/panel/api/reseller/me/totp/disable', { code })),
  });
  return { changePassword, enableTotp, disableTotp };
}
