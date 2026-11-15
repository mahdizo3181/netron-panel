import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import type {
  ResellerAuditPage,
  ResellerMe,
  ResellerPoolEnforceView,
  ResellerView,
} from '@/generated/types';

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
  const addVolume = useMutation({
    mutationFn: async ({ id, bytes }: { id: number; bytes: number }) =>
      done(await HttpUtil.post(`/panel/api/resellers/${id}/addVolume`, { bytes }, JSON_HEADERS)),
  });
  const resetUsage = useMutation({
    mutationFn: async (id: number) =>
      done(await HttpUtil.post(`/panel/api/resellers/${id}/resetUsage`, {}, JSON_HEADERS)),
  });
  const disableClients = useMutation({
    mutationFn: async (id: number) =>
      done(await HttpUtil.post(`/panel/api/resellers/${id}/disableClients`, {}, JSON_HEADERS)),
  });
  const enableClients = useMutation({
    mutationFn: async (id: number) =>
      done(await HttpUtil.post(`/panel/api/resellers/${id}/enableClients`, {}, JSON_HEADERS)),
  });
  const setPoolEnforce = useMutation({
    mutationFn: async (on: boolean) =>
      done(await HttpUtil.post('/panel/api/resellers/poolEnforce', { on }, JSON_HEADERS)),
  });
  const extendExpiry = useMutation({
    mutationFn: async ({ id, expiryTime }: { id: number; expiryTime: number }) =>
      done(
        await HttpUtil.post(
          `/panel/api/resellers/${id}/extendExpiry`,
          { expiryTime },
          JSON_HEADERS,
        ),
      ),
  });
  return {
    create,
    update,
    setPassword,
    remove,
    resetTotp,
    addVolume,
    resetUsage,
    disableClients,
    enableClients,
    setPoolEnforce,
    extendExpiry,
  };
}

export function usePoolEnforce(enabled = true) {
  return useQuery({
    queryKey: [...keys.resellers.root(), 'pool-enforce'],
    enabled,
    queryFn: async (): Promise<boolean> => {
      const msg = await HttpUtil.get<ResellerPoolEnforceView>(
        '/panel/api/resellers/poolEnforce',
        undefined,
        { silent: true },
      );
      if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Failed to load pool enforcement');
      return msg.obj.on;
    },
  });
}

export interface ResellerAuditQuery {
  resellerId?: number;
  self?: boolean;
  page?: number;
  pageSize?: number;
  action?: string;
  from?: number;
  to?: number;
}

export function useResellerAuditList(params: ResellerAuditQuery) {
  const { resellerId, self, page = 1, pageSize = 25, action, from, to } = params;
  return useQuery({
    queryKey: [...keys.resellers.root(), 'audit', params],
    enabled: self || (resellerId ?? 0) > 0,
    queryFn: async (): Promise<ResellerAuditPage> => {
      const path = self
        ? '/panel/api/reseller/me/audit'
        : `/panel/api/resellers/${resellerId}/audit`;
      const msg = await HttpUtil.get<ResellerAuditPage>(
        path,
        { page, pageSize, action, from, to },
        { silent: true },
      );
      if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Failed to load audit log');
      return msg.obj;
    },
  });
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
