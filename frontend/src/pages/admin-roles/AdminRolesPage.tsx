import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Checkbox,
  ConfigProvider,
  Form,
  Input,
  InputNumber,
  Layout,
  Modal,
  Result,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  message,
} from 'antd';
import type { TableColumnsType } from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';

import AppSidebar from '@/layouts/AppSidebar';
import { useTheme } from '@/hooks/useTheme';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { usePageTitle } from '@/hooks/usePageTitle';
import { useCurrentAdmin } from '@/hooks/useCurrentAdmin';
import { keys } from '@/api/queryKeys';
import { HttpUtil } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import { parseMsg } from '@/utils/zodValidate';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

const PermissionMapSchema = z.record(z.string(), z.unknown());

const AdminRoleSchema = z.object({
  id: z.number(),
  name: z.string(),
  slug: z.string(),
  builtIn: z.boolean(),
  ownerRole: z.boolean(),
  permissions: PermissionMapSchema.optional(),
  limits: PermissionMapSchema.optional(),
});

const AdminRoleListSchema = z.array(AdminRoleSchema);

type AdminRoleRecord = z.infer<typeof AdminRoleSchema>;
type PermissionRoot = Record<string, Record<string, unknown>>;

const BUILTIN_READONLY_SLUGS = new Set(['owner', 'administrator']);

interface RoleFormValues {
  name: string;
  slug: string;
  permissions: PermissionRoot;
}

interface RoleLimitsForm {
  maxUsers?: number | null;
  minDataLimitGb?: number | null;
  maxDataLimitGb?: number | null;
  minExpireDays?: number | null;
  maxExpireDays?: number | null;
}

function positiveLimitNum(
  limits: Record<string, unknown> | undefined,
  key: string,
): number | undefined {
  const v = limits?.[key];
  if (typeof v === 'number' && v > 0) return v;
  if (typeof v === 'string' && v.trim() !== '') {
    const n = Number(v);
    return n > 0 ? n : undefined;
  }
  return undefined;
}

function limitsToForm(limits?: Record<string, unknown>): RoleLimitsForm {
  if (!limits) return {};
  return {
    maxUsers: positiveLimitNum(limits, 'maxUsers'),
    minDataLimitGb: positiveLimitNum(limits, 'minDataLimit'),
    maxDataLimitGb: positiveLimitNum(limits, 'maxDataLimit'),
    minExpireDays: positiveLimitNum(limits, 'minExpireDays'),
    maxExpireDays: positiveLimitNum(limits, 'maxExpireDays'),
  };
}

function formToLimits(form: RoleLimitsForm): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (form.maxUsers != null && form.maxUsers > 0) out.maxUsers = form.maxUsers;
  if (form.minDataLimitGb != null && form.minDataLimitGb > 0)
    out.minDataLimit = form.minDataLimitGb;
  if (form.maxDataLimitGb != null && form.maxDataLimitGb > 0)
    out.maxDataLimit = form.maxDataLimitGb;
  if (form.minExpireDays != null && form.minExpireDays > 0) out.minExpireDays = form.minExpireDays;
  if (form.maxExpireDays != null && form.maxExpireDays > 0) out.maxExpireDays = form.maxExpireDays;
  return out;
}

function defaultPermissions(): PermissionRoot {
  return {
    users: { read: 'own', create: false, update: 'own', delete: 'own', reset_usage: 'own' },
    admins: { read: false, create: false, update: false, delete: false, reset_usage: false },
    inbounds: { read_simple: false },
    settings: { read_general: false },
    nodes: { read: false },
  };
}

function normalizeRolePermissions(raw?: Record<string, unknown>): PermissionRoot {
  const base = defaultPermissions();
  if (!raw) return base;
  for (const [section, value] of Object.entries(raw)) {
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      base[section] = { ...(base[section] ?? {}), ...(value as Record<string, unknown>) };
    }
  }
  return base;
}

function scopeValue(raw: unknown): 'all' | 'own' | 'off' {
  if (raw === true || raw === 'all' || raw === '2' || raw === 2) return 'all';
  if (raw === 'own' || raw === '1' || raw === 1) return 'own';
  if (raw && typeof raw === 'object' && 'scope' in raw) {
    return scopeValue((raw as { scope: unknown }).scope);
  }
  return 'off';
}

function setScope(
  permissions: PermissionRoot,
  section: string,
  key: string,
  scope: 'all' | 'own' | 'off',
) {
  const next = { ...permissions, [section]: { ...(permissions[section] ?? {}) } };
  if (scope === 'off') {
    next[section][key] = false;
  } else if (scope === 'all') {
    next[section][key] = 'all';
  } else {
    next[section][key] = 'own';
  }
  return next;
}

function setBool(permissions: PermissionRoot, section: string, key: string, value: boolean) {
  const next = { ...permissions, [section]: { ...(permissions[section] ?? {}) } };
  next[section][key] = value;
  return next;
}

export default function AdminRolesPage() {
  usePageTitle();
  const { t } = useTranslation();
  const { antdThemeConfig } = useTheme();
  const { isMobile } = useMediaQuery();
  const { hasPermission } = useCurrentAdmin();
  const [modal, modalContextHolder] = Modal.useModal();
  const [messageApi, messageContextHolder] = message.useMessage();
  const queryClient = useQueryClient();

  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);

  const canCreate = hasPermission('admins', 'create');
  const canUpdate = hasPermission('admins', 'update');
  const canDelete = hasPermission('admins', 'delete');

  const [formOpen, setFormOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<AdminRoleRecord | null>(null);
  const [formValues, setFormValues] = useState<RoleFormValues>({
    name: '',
    slug: '',
    permissions: defaultPermissions(),
  });
  const [limitsForm, setLimitsForm] = useState<RoleLimitsForm>({});

  const rolesQuery = useQuery({
    queryKey: keys.adminRoles.list(),
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/adminRoles/list', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to load admin roles');
      const validated = parseMsg(msg, AdminRoleListSchema, 'adminRoles/list');
      return validated.obj ?? [];
    },
  });

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: keys.adminRoles.root() });
  }, [queryClient]);

  const saveMut = useMutation({
    mutationFn: async (body: Record<string, unknown>) => {
      if (editTarget) {
        return HttpUtil.post(`/panel/api/adminRoles/update/${editTarget.id}`, body, JSON_HEADERS);
      }
      return HttpUtil.post('/panel/api/adminRoles/add', body, JSON_HEADERS);
    },
    onSuccess: (msg) => {
      if (msg?.success) {
        invalidate();
        setFormOpen(false);
        setEditTarget(null);
        setLimitsForm({});
        return;
      }
      messageApi.error(msg?.msg || t('pages.adminRoles.saveFailed'));
    },
  });

  const deleteMut = useMutation({
    mutationFn: (id: number) => HttpUtil.post(`/panel/api/adminRoles/del/${id}`, {}, JSON_HEADERS),
    onSuccess: (msg) => {
      if (msg?.success) invalidate();
    },
  });

  const roles = rolesQuery.data ?? [];
  const matrixReadOnly =
    editTarget != null &&
    (editTarget.builtIn ||
      BUILTIN_READONLY_SLUGS.has(editTarget.slug) ||
      (!canUpdate && !canCreate));

  function openCreate() {
    setEditTarget(null);
    setFormValues({ name: '', slug: '', permissions: defaultPermissions() });
    setLimitsForm({});
    setFormOpen(true);
  }

  function openEdit(role: AdminRoleRecord) {
    setEditTarget(role);
    setFormValues({
      name: role.name,
      slug: role.slug,
      permissions: normalizeRolePermissions(role.permissions),
    });
    setLimitsForm(limitsToForm(role.limits));
    setFormOpen(true);
  }

  async function submitForm() {
    const name = formValues.name.trim();
    const slug = formValues.slug.trim().toLowerCase();
    if (!name || !slug) {
      messageApi.error(t('pages.adminRoles.nameSlugRequired'));
      return;
    }
    await saveMut.mutateAsync({
      name,
      slug: editTarget?.builtIn ? editTarget.slug : slug,
      permissions: formValues.permissions,
      limits: formToLimits(limitsForm),
    });
  }

  const columns: TableColumnsType<AdminRoleRecord> = useMemo(
    () => [
      {
        title: t('pages.adminRoles.name'),
        dataIndex: 'name',
        render: (name, row) => (
          <Space>
            <span>{name}</span>
            {row.builtIn && <Tag color="blue">{t('pages.adminRoles.builtIn')}</Tag>}
            {row.ownerRole && <Tag color="gold">{t('pages.adminRoles.owner')}</Tag>}
          </Space>
        ),
      },
      { title: t('pages.adminRoles.slug'), dataIndex: 'slug' },
      {
        title: t('pages.adminRoles.actions'),
        width: 160,
        render: (_, row) => (
          <Space>
            <Button type="link" icon={<EditOutlined aria-hidden />} onClick={() => openEdit(row)}>
              {matrixReadOnly && row.builtIn
                ? t('pages.adminRoles.view')
                : t('pages.adminRoles.edit')}
            </Button>
            {!row.builtIn && canDelete && (
              <Button
                type="link"
                danger
                icon={<DeleteOutlined aria-hidden />}
                onClick={() =>
                  modal.confirm({
                    title: t('pages.adminRoles.deleteConfirmTitle', { name: row.name }),
                    okType: 'danger',
                    onOk: () => deleteMut.mutateAsync(row.id),
                  })
                }
              >
                {t('pages.adminRoles.delete')}
              </Button>
            )}
          </Space>
        ),
      },
    ],
    [canDelete, deleteMut, matrixReadOnly, modal, t],
  );

  const permissions = formValues.permissions;
  const sensitiveReadOnly = editTarget != null && BUILTIN_READONLY_SLUGS.has(editTarget.slug);

  const userScopeSelect = (key: string, label: string) => (
    <Form.Item name={`users-${key}`} label={label} style={{ marginBottom: 8 }}>
      <Select
        disabled={matrixReadOnly || sensitiveReadOnly}
        value={scopeValue(permissions.users?.[key])}
        onChange={(v) =>
          setFormValues((prev) => ({
            ...prev,
            permissions: setScope(prev.permissions, 'users', key, v),
          }))
        }
        options={[
          { value: 'off', label: t('pages.adminRoles.scopeOff') },
          { value: 'own', label: t('pages.adminRoles.scopeOwn') },
          { value: 'all', label: t('pages.adminRoles.scopeAll') },
        ]}
      />
    </Form.Item>
  );

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      {modalContextHolder}
      <Layout style={{ minHeight: '100vh' }}>
        <AppSidebar />
        <Layout.Content style={{ padding: isMobile ? 12 : 24 }}>
          {rolesQuery.isLoading ? (
            <div style={{ textAlign: 'center', padding: 48 }}>
              <Spin size="large" />
            </div>
          ) : rolesQuery.isError ? (
            <Result status="error" title={t('pages.adminRoles.loadError')} />
          ) : (
            <Card
              title={t('menu.adminRoles')}
              extra={
                canCreate && (
                  <Button type="primary" icon={<PlusOutlined aria-hidden />} onClick={openCreate}>
                    {t('pages.adminRoles.add')}
                  </Button>
                )
              }
            >
              <Table
                rowKey="id"
                columns={columns}
                dataSource={roles}
                pagination={false}
                scroll={{ x: 600 }}
              />
            </Card>
          )}

          <Modal
            title={
              editTarget
                ? t('pages.adminRoles.editTitle', { name: editTarget.name })
                : t('pages.adminRoles.add')
            }
            open={formOpen}
            onCancel={() => {
              setFormOpen(false);
              setEditTarget(null);
            }}
            onOk={() => void submitForm()}
            confirmLoading={saveMut.isPending}
            destroyOnHidden
            width={720}
            okButtonProps={{ disabled: matrixReadOnly && !!editTarget?.builtIn }}
          >
            <Form layout="vertical">
              <Form.Item name="roleName" label={t('pages.adminRoles.name')} required>
                <Input
                  value={formValues.name}
                  disabled={matrixReadOnly || !!editTarget?.builtIn}
                  onChange={(e) => setFormValues((v) => ({ ...v, name: e.target.value }))}
                />
              </Form.Item>
              <Form.Item name="roleSlug" label={t('pages.adminRoles.slug')} required>
                <Input
                  value={formValues.slug}
                  disabled={matrixReadOnly || !!editTarget?.builtIn}
                  onChange={(e) => setFormValues((v) => ({ ...v, slug: e.target.value }))}
                />
              </Form.Item>

              <Card size="small" title={t('pages.adminRoles.usersSection')} type="inner">
                {userScopeSelect('read', t('pages.adminRoles.usersRead'))}
                <Form.Item
                  name="users-create"
                  label={t('pages.adminRoles.usersCreate')}
                  style={{ marginBottom: 8 }}
                >
                  <Switch
                    disabled={matrixReadOnly || sensitiveReadOnly}
                    checked={!!permissions.users?.create}
                    onChange={(checked) =>
                      setFormValues((prev) => ({
                        ...prev,
                        permissions: setBool(prev.permissions, 'users', 'create', checked),
                      }))
                    }
                  />
                </Form.Item>
                {userScopeSelect('update', t('pages.adminRoles.usersUpdate'))}
                {userScopeSelect('delete', t('pages.adminRoles.usersDelete'))}
                {userScopeSelect('reset_usage', t('pages.adminRoles.usersResetUsage'))}
              </Card>

              <Card
                size="small"
                title={t('pages.adminRoles.inboundsSection')}
                type="inner"
                style={{ marginTop: 12 }}
              >
                <Checkbox
                  disabled={matrixReadOnly || sensitiveReadOnly}
                  checked={!!permissions.inbounds?.read_simple}
                  onChange={(e) =>
                    setFormValues((prev) => ({
                      ...prev,
                      permissions: setBool(
                        prev.permissions,
                        'inbounds',
                        'read_simple',
                        e.target.checked,
                      ),
                    }))
                  }
                >
                  {t('pages.adminRoles.inboundsReadSimple')}
                </Checkbox>
              </Card>

              <Card
                size="small"
                title={t('pages.adminRoles.settingsSection')}
                type="inner"
                style={{ marginTop: 12 }}
              >
                <Checkbox
                  disabled={matrixReadOnly || sensitiveReadOnly}
                  checked={!!permissions.settings?.read_general}
                  onChange={(e) =>
                    setFormValues((prev) => ({
                      ...prev,
                      permissions: setBool(
                        prev.permissions,
                        'settings',
                        'read_general',
                        e.target.checked,
                      ),
                    }))
                  }
                >
                  {t('pages.adminRoles.settingsReadGeneral')}
                </Checkbox>
              </Card>

              <Card
                size="small"
                title={t('pages.adminRoles.nodesSection')}
                type="inner"
                style={{ marginTop: 12 }}
              >
                <Checkbox
                  disabled={matrixReadOnly || sensitiveReadOnly}
                  checked={!!permissions.nodes?.read}
                  onChange={(e) =>
                    setFormValues((prev) => ({
                      ...prev,
                      permissions: setBool(prev.permissions, 'nodes', 'read', e.target.checked),
                    }))
                  }
                >
                  {t('pages.adminRoles.nodesRead')}
                </Checkbox>
              </Card>

              <Card
                size="small"
                title={t('pages.adminRoles.limitsSection')}
                type="inner"
                style={{ marginTop: 12 }}
              >
                <Form.Item
                  name="roleMaxUsers"
                  label={t('pages.admins.maxUsers')}
                  style={{ marginBottom: 8 }}
                >
                  <InputNumber
                    min={0}
                    style={{ width: '100%' }}
                    disabled={matrixReadOnly || sensitiveReadOnly}
                    value={limitsForm.maxUsers ?? undefined}
                    onChange={(v) => setLimitsForm((prev) => ({ ...prev, maxUsers: v }))}
                  />
                </Form.Item>
                <Form.Item
                  name="roleMinDataLimitGb"
                  label={t('pages.admins.dataLimitMinGb')}
                  style={{ marginBottom: 8 }}
                >
                  <InputNumber
                    min={0}
                    style={{ width: '100%' }}
                    disabled={matrixReadOnly || sensitiveReadOnly}
                    value={limitsForm.minDataLimitGb ?? undefined}
                    onChange={(v) => setLimitsForm((prev) => ({ ...prev, minDataLimitGb: v }))}
                  />
                </Form.Item>
                <Form.Item
                  name="roleMaxDataLimitGb"
                  label={t('pages.admins.dataLimitMaxGb')}
                  style={{ marginBottom: 8 }}
                >
                  <InputNumber
                    min={0}
                    style={{ width: '100%' }}
                    disabled={matrixReadOnly || sensitiveReadOnly}
                    value={limitsForm.maxDataLimitGb ?? undefined}
                    onChange={(v) => setLimitsForm((prev) => ({ ...prev, maxDataLimitGb: v }))}
                  />
                </Form.Item>
                <Form.Item
                  name="roleMinExpireDays"
                  label={t('pages.admins.expireDaysMin')}
                  style={{ marginBottom: 8 }}
                >
                  <InputNumber
                    min={0}
                    style={{ width: '100%' }}
                    disabled={matrixReadOnly || sensitiveReadOnly}
                    value={limitsForm.minExpireDays ?? undefined}
                    onChange={(v) => setLimitsForm((prev) => ({ ...prev, minExpireDays: v }))}
                  />
                </Form.Item>
                <Form.Item
                  name="roleMaxExpireDays"
                  label={t('pages.admins.expireDaysMax')}
                  style={{ marginBottom: 0 }}
                >
                  <InputNumber
                    min={0}
                    style={{ width: '100%' }}
                    disabled={matrixReadOnly || sensitiveReadOnly}
                    value={limitsForm.maxExpireDays ?? undefined}
                    onChange={(v) => setLimitsForm((prev) => ({ ...prev, maxExpireDays: v }))}
                  />
                </Form.Item>
              </Card>

              <Card
                size="small"
                title={t('pages.adminRoles.adminsSection')}
                type="inner"
                style={{ marginTop: 12 }}
              >
                {(['read', 'create', 'update', 'delete', 'reset_usage'] as const).map((key) => (
                  <Form.Item
                    key={key}
                    name={`admins-${key}`}
                    label={t(`pages.adminRoles.admins_${key}`)}
                    style={{ marginBottom: 8 }}
                  >
                    <Switch
                      disabled={matrixReadOnly || sensitiveReadOnly}
                      checked={!!permissions.admins?.[key]}
                      onChange={(checked) =>
                        setFormValues((prev) => ({
                          ...prev,
                          permissions: setBool(prev.permissions, 'admins', key, checked),
                        }))
                      }
                    />
                  </Form.Item>
                ))}
              </Card>

              {editTarget?.builtIn && (
                <Tag
                  icon={<SafetyCertificateOutlined aria-hidden />}
                  color="processing"
                  style={{ marginTop: 12 }}
                >
                  {t('pages.adminRoles.builtInHint')}
                </Tag>
              )}
            </Form>
          </Modal>
        </Layout.Content>
      </Layout>
    </ConfigProvider>
  );
}
