import { useCallback, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { z } from 'zod';

import { keys } from '@/api/queryKeys';
import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';

const PermissionMapSchema = z.preprocess(
  (value) => {
    if (typeof value === 'string') {
      try {
        return JSON.parse(value) as unknown;
      } catch {
        return {};
      }
    }
    return value;
  },
  z.record(z.string(), z.unknown()),
);

const CurrentAdminRoleSchema = z.object({
  id: z.number(),
  name: z.string(),
  slug: z.string(),
  ownerRole: z.boolean(),
  permissions: PermissionMapSchema.optional(),
  limits: PermissionMapSchema.optional(),
});

const CurrentAdminSchema = z.object({
  id: z.number(),
  username: z.string(),
  status: z.string(),
  roleId: z.number(),
  dataLimit: z.number(),
  usedBytes: z.number(),
  permissions: PermissionMapSchema.optional(),
  limits: PermissionMapSchema.optional(),
  role: CurrentAdminRoleSchema,
});

export type CurrentAdmin = z.infer<typeof CurrentAdminSchema>;
export type AdminPermissionRoot = Record<string, Record<string, unknown>>;

const PERMISSION_ALIASES: Record<string, string[]> = {
  read: ['read', 'view'],
  view: ['view', 'read'],
  reset_usage: ['reset_usage', 'resetUsage'],
  resetUsage: ['resetUsage', 'reset_usage'],
  read_simple: ['read_simple', 'read'],
  read_general: ['read_general', 'read'],
};

function permissionValueAllowed(value: unknown): boolean {
  if (value === null || value === undefined) return false;
  if (typeof value === 'boolean') return value;
  if (typeof value === 'number') return value !== 0;
  if (typeof value === 'string') {
    switch (value.trim().toLowerCase()) {
      case 'true':
      case 'yes':
      case '1':
      case 'own':
      case 'all':
        return true;
      default:
        return false;
    }
  }
  if (typeof value === 'object' && value !== null && 'scope' in value) {
    return permissionValueAllowed((value as { scope: unknown }).scope);
  }
  return false;
}

function normalizePermissions(root: Record<string, unknown> | undefined): AdminPermissionRoot {
  if (!root) return {};
  const out: AdminPermissionRoot = {};
  for (const [section, value] of Object.entries(root)) {
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      out[section] = value as Record<string, unknown>;
    }
  }
  return out;
}

async function fetchCurrentAdmin(): Promise<CurrentAdmin> {
  const msg = await HttpUtil.get('/panel/api/admins/current', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to load current admin');
  const validated = parseMsg(msg, CurrentAdminSchema, 'admins/current');
  if (!validated.obj) throw new Error('Invalid current admin payload');
  return validated.obj;
}

export function useCurrentAdmin() {
  const query = useQuery({
    queryKey: keys.admins.current(),
    queryFn: fetchCurrentAdmin,
    staleTime: 60_000,
  });

  const admin = query.data;
  const isOwner = admin?.role?.ownerRole ?? false;

  const permissions = useMemo(() => {
    const merged = {
      ...normalizePermissions(admin?.role?.permissions as Record<string, unknown> | undefined),
      ...normalizePermissions(admin?.permissions as Record<string, unknown> | undefined),
    };
    return merged;
  }, [admin?.permissions, admin?.role?.permissions]);

  const hasPermission = useCallback(
    (section: string, permission: string): boolean => {
      if (isOwner) return true;
      const sectionMap = permissions[section];
      if (!sectionMap) return false;
      const keysForPerm = PERMISSION_ALIASES[permission] ?? [permission];
      return keysForPerm.some((key) => permissionValueAllowed(sectionMap[key]));
    },
    [isOwner, permissions],
  );

  const canAccessPanelRoute = useCallback(
    (path: string): boolean => {
      if (isOwner) return true;
      switch (path) {
        case '/admins':
        case '/admin-roles':
          return hasPermission('admins', 'read');
        case '/nodes':
          return hasPermission('nodes', 'read');
        case '/inbounds':
        case '/hosts':
        case '/xray':
        case '/outbound':
        case '/routing':
          return hasPermission('inbounds', 'read') || hasPermission('inbounds', 'read_simple');
        case '/settings':
          return hasPermission('settings', 'read_general');
        case '/clients':
        case '/groups':
          return hasPermission('users', 'read');
        default:
          return true;
      }
    },
    [hasPermission, isOwner],
  );

  return {
    admin,
    isOwner,
    permissions,
    hasPermission,
    canAccessPanelRoute,
    isLoading: query.isLoading,
    isFetching: query.isFetching,
    error: query.error,
    refetch: query.refetch,
  };
}
