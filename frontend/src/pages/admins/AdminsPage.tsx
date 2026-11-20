import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  ConfigProvider,
  Dropdown,
  Form,
  Input,
  InputNumber,
  Layout,
  Modal,
  Progress,
  Result,
  Row,
  Select,
  Space,
  Spin,
  Statistic,
  Table,
  Tag,
  Tooltip,
  message,
} from 'antd';
import type { MenuProps, TableColumnsType, TablePaginationConfig } from 'antd';
import {
  CheckCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  MinusCircleOutlined,
  MoreOutlined,
  PlusOutlined,
  ReloadOutlined,
  StopOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';

import AppSidebar from '@/layouts/AppSidebar';
import { useTheme } from '@/hooks/useTheme';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { usePageTitle } from '@/hooks/usePageTitle';
import { useCurrentAdmin } from '@/hooks/useCurrentAdmin';
import { keys } from '@/api/queryKeys';
import { HttpUtil, SizeFormatter } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import { parseMsg } from '@/utils/zodValidate';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

const AdminViewSchema = z.object({
  id: z.number(),
  username: z.string(),
  roleId: z.number(),
  roleName: z.string(),
  roleSlug: z.string(),
  status: z.string(),
  dataLimit: z.number(),
  usedBytes: z.number(),
  totalUsers: z.number(),
  permissionOverrides: z.record(z.string(), z.unknown()).optional(),
});

const AdminListSchema = z.array(AdminViewSchema);

const AdminStatsSchema = z.object({
  total: z.number(),
  active: z.number(),
  disabled: z.number(),
  limited: z.number(),
});

const AdminRoleOptionSchema = z.object({
  id: z.number(),
  name: z.string(),
  slug: z.string(),
  builtIn: z.boolean(),
  ownerRole: z.boolean(),
});

const AdminRoleListSchema = z.array(AdminRoleOptionSchema);

type AdminRecord = z.infer<typeof AdminViewSchema>;
type AdminStats = z.infer<typeof AdminStatsSchema>;
interface AdminFormValues {
  username: string;
  password?: string;
  roleId: number;
  status: string;
  dataLimitGb?: number | null;
  maxUsers?: number | null;
  dataLimitMinGb?: number | null;
  dataLimitMaxGb?: number | null;
  expireDaysMin?: number | null;
  expireDaysMax?: number | null;
}

function gbToBytes(gb: number | null | undefined): number | undefined {
  if (gb === null || gb === undefined || Number.isNaN(gb)) return undefined;
  if (gb <= 0) return undefined;
  return Math.round(gb * 1073741824);
}

function bytesToGb(bytes: number): number | null {
  if (bytes <= 0) return null;
  return Math.round((bytes / 1073741824) * 100) / 100;
}

function buildOverrides(values: AdminFormValues): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (values.maxUsers != null && values.maxUsers > 0) out.max_users = values.maxUsers;
  if (values.dataLimitMinGb != null && values.dataLimitMinGb > 0) {
    out.minDataLimit = values.dataLimitMinGb;
  }
  if (values.dataLimitMaxGb != null && values.dataLimitMaxGb > 0) {
    out.maxDataLimit = values.dataLimitMaxGb;
  }
  if (values.expireDaysMin != null && values.expireDaysMin > 0) {
    out.expire_days_min = values.expireDaysMin;
  }
  if (values.expireDaysMax != null && values.expireDaysMax > 0) {
    out.expire_days_max = values.expireDaysMax;
  }
  return out;
}

function overridesToForm(overrides: Record<string, unknown> | undefined): Partial<AdminFormValues> {
  if (!overrides) return {};
  const num = (key: string) => {
    const v = overrides[key];
    return typeof v === 'number' ? v : typeof v === 'string' ? Number(v) || undefined : undefined;
  };
  return {
    maxUsers: num('max_users'),
    dataLimitMinGb:
      num('minDataLimit') ??
      (num('data_limit_min') ? bytesToGb(num('data_limit_min')!) : undefined),
    dataLimitMaxGb:
      num('maxDataLimit') ??
      (num('data_limit_max') ? bytesToGb(num('data_limit_max')!) : undefined),
    expireDaysMin: num('expire_days_min') ?? num('minExpireDays'),
    expireDaysMax: num('expire_days_max') ?? num('maxExpireDays'),
  };
}

export default function AdminsPage() {
  usePageTitle();
  const { t } = useTranslation();
  const { isDark, antdThemeConfig } = useTheme();
  const { isMobile } = useMediaQuery();
  const { hasPermission } = useCurrentAdmin();
  const [modal, modalContextHolder] = Modal.useModal();
  const [messageApi, messageContextHolder] = message.useMessage();
  const [form] = Form.useForm<AdminFormValues>();
  const queryClient = useQueryClient();

  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);

  const canCreate = hasPermission('admins', 'create');
  const canUpdate = hasPermission('admins', 'update');
  const canDelete = hasPermission('admins', 'delete');
  const canResetUsage = hasPermission('admins', 'reset_usage');

  const [search, setSearch] = useState('');
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  const [formOpen, setFormOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<AdminRecord | null>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);

  const adminsQuery = useQuery({
    queryKey: keys.admins.list(),
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/admins/list', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to load admins');
      const validated = parseMsg(msg, AdminListSchema, 'admins/list');
      return validated.obj ?? [];
    },
  });

  const statsQuery = useQuery({
    queryKey: keys.admins.stats(),
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/admins/stats', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to load admin stats');
      const validated = parseMsg(msg, AdminStatsSchema, 'admins/stats');
      if (!validated.obj) throw new Error('Invalid admin stats');
      return validated.obj;
    },
  });

  const rolesQuery = useQuery({
    queryKey: keys.adminRoles.list(),
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/adminRoles/list', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to load roles');
      const validated = parseMsg(msg, AdminRoleListSchema, 'adminRoles/list');
      return (validated.obj ?? []).filter((r) => !r.ownerRole);
    },
  });

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: keys.admins.root() });
  }, [queryClient]);

  const saveMut = useMutation({
    mutationFn: async (body: Record<string, unknown>) => {
      if (editTarget) {
        return HttpUtil.post(`/panel/api/admins/update/${editTarget.id}`, body, JSON_HEADERS);
      }
      return HttpUtil.post('/panel/api/admins/add', body, JSON_HEADERS);
    },
    onSuccess: (msg) => {
      if (msg?.success) {
        invalidate();
        setFormOpen(false);
        setEditTarget(null);
        form.resetFields();
      }
    },
  });

  const actionMut = useMutation({
    mutationFn: (path: string) => HttpUtil.post(path, {}, JSON_HEADERS),
    onSuccess: (msg) => {
      if (msg?.success) invalidate();
    },
  });

  const admins = adminsQuery.data;
  const stats: AdminStats | undefined = statsQuery.data;
  const roleOptions = rolesQuery.data ?? [];

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const rows = admins ?? [];
    if (!q) return rows;
    return rows.filter(
      (row) =>
        row.username.toLowerCase().includes(q) ||
        row.roleName.toLowerCase().includes(q) ||
        row.roleSlug.toLowerCase().includes(q),
    );
  }, [admins, search]);

  const paged = useMemo(() => {
    const start = (page - 1) * pageSize;
    return filtered.slice(start, start + pageSize);
  }, [filtered, page, pageSize]);

  const ownerSlug = 'owner';

  function openCreate() {
    setEditTarget(null);
    form.resetFields();
    form.setFieldsValue({ status: 'active' });
    setFormOpen(true);
  }

  function openEdit(record: AdminRecord) {
    if (record.roleSlug === ownerSlug) return;
    setEditTarget(record);
    form.setFieldsValue({
      username: record.username,
      roleId: record.roleId,
      status: record.status,
      dataLimitGb: bytesToGb(record.dataLimit),
      password: '',
      ...overridesToForm(record.permissionOverrides),
    });
    setFormOpen(true);
  }

  async function submitForm() {
    const values = await form.validateFields();
    const dataLimitBytes =
      values.dataLimitGb != null && values.dataLimitGb > 0
        ? (gbToBytes(values.dataLimitGb) ?? 0)
        : 0;
    const body: Record<string, unknown> = {
      username: values.username.trim(),
      roleId: values.roleId,
      status: values.status,
      dataLimit: dataLimitBytes,
      permissionOverrides: buildOverrides(values),
    };
    if (!editTarget || values.password?.trim()) {
      body.password = values.password?.trim() || '';
    }
    if (!editTarget && !body.password) {
      messageApi.error(t('pages.admins.passwordRequired'));
      return;
    }
    await saveMut.mutateAsync(body);
  }

  function confirmAction(title: string, content: string, path: string) {
    modal.confirm({
      title,
      content,
      okType: 'danger',
      onOk: () => actionMut.mutateAsync(path),
    });
  }

  function runBulk(makePath: (id: number) => string) {
    const ids = selectedIds;
    if (ids.length === 0) return;
    modal.confirm({
      title: t('pages.admins.bulkConfirmTitle', { count: ids.length }),
      onOk: async () => {
        for (const id of ids) {
          await actionMut.mutateAsync(makePath(id));
        }
        setSelectedIds([]);
      },
    });
  }

  const rowActions = (record: AdminRecord): MenuProps['items'] => {
    if (record.roleSlug === ownerSlug) return [];
    const id = record.id;
    return [
      canUpdate && {
        key: 'edit',
        icon: <EditOutlined aria-hidden />,
        label: t('pages.admins.edit'),
        onClick: () => openEdit(record),
      },
      canUpdate && record.status === 'active'
        ? {
            key: 'disable',
            icon: <StopOutlined aria-hidden />,
            label: t('pages.admins.disable'),
            onClick: () =>
              confirmAction(
                t('pages.admins.disableConfirmTitle', { name: record.username }),
                t('pages.admins.disableConfirmContent'),
                `/panel/api/admins/disable/${id}`,
              ),
          }
        : canUpdate && {
            key: 'enable',
            icon: <CheckCircleOutlined aria-hidden />,
            label: t('pages.admins.enable'),
            onClick: () => actionMut.mutateAsync(`/panel/api/admins/enable/${id}`),
          },
      canResetUsage && {
        key: 'reset',
        icon: <ReloadOutlined aria-hidden />,
        label: t('pages.admins.resetUsage'),
        onClick: () =>
          confirmAction(
            t('pages.admins.resetUsageConfirmTitle', { name: record.username }),
            t('pages.admins.resetUsageConfirmContent'),
            `/panel/api/admins/resetUsage/${id}`,
          ),
      },
      canUpdate && {
        key: 'disableClients',
        icon: <MinusCircleOutlined aria-hidden />,
        label: t('pages.admins.disableAllClients'),
        onClick: () =>
          confirmAction(
            t('pages.admins.disableClientsTitle', { name: record.username }),
            t('pages.admins.disableClientsContent'),
            `/panel/api/admins/users/disableActive/${id}`,
          ),
      },
      canUpdate && {
        key: 'activateClients',
        icon: <CheckCircleOutlined aria-hidden />,
        label: t('pages.admins.activateQuotaDisabledClients'),
        onClick: () => actionMut.mutateAsync(`/panel/api/admins/users/activateDisabled/${id}`),
      },
      canUpdate && {
        key: 'removeClients',
        icon: <TeamOutlined aria-hidden />,
        label: t('pages.admins.removeAllClients'),
        onClick: () =>
          confirmAction(
            t('pages.admins.removeClientsTitle', { name: record.username }),
            t('pages.admins.removeClientsContent', { count: record.totalUsers }),
            `/panel/api/admins/users/removeAll/${id}`,
          ),
      },
      canDelete && {
        key: 'delete',
        danger: true,
        icon: <DeleteOutlined aria-hidden />,
        label: t('pages.admins.delete'),
        onClick: () =>
          confirmAction(
            t('pages.admins.deleteConfirmTitle', { name: record.username }),
            t('pages.admins.deleteConfirmContent'),
            `/panel/api/admins/del/${id}`,
          ),
      },
    ].filter(Boolean) as MenuProps['items'];
  };

  const columns: TableColumnsType<AdminRecord> = [
    {
      title: t('pages.admins.username'),
      dataIndex: 'username',
      sorter: (a, b) => a.username.localeCompare(b.username),
    },
    {
      title: t('pages.admins.role'),
      render: (_, row) => (
        <Space size={4}>
          <span>{row.roleName}</span>
          <Tag>{row.roleSlug}</Tag>
        </Space>
      ),
    },
    {
      title: t('pages.admins.status'),
      dataIndex: 'status',
      render: (status: string) => (
        <Tag color={status === 'active' ? 'success' : 'default'}>
          {status === 'active' ? t('pages.admins.active') : t('pages.admins.disabled')}
        </Tag>
      ),
    },
    {
      title: t('pages.admins.clients'),
      dataIndex: 'totalUsers',
      width: 100,
    },
    {
      title: t('pages.admins.usage'),
      render: (_, row) => {
        const unlimited = row.dataLimit <= 0;
        const percent = unlimited
          ? 0
          : Math.min(100, Math.round((row.usedBytes / row.dataLimit) * 100));
        return (
          <Tooltip
            title={
              unlimited
                ? t('pages.admins.unlimitedQuota')
                : `${SizeFormatter.sizeFormat(row.usedBytes)} / ${SizeFormatter.sizeFormat(row.dataLimit)}`
            }
          >
            {unlimited ? (
              <Tag>{t('pages.admins.unlimitedQuota')}</Tag>
            ) : (
              <Progress
                percent={percent}
                size="small"
                status={percent >= 100 ? 'exception' : 'normal'}
              />
            )}
          </Tooltip>
        );
      },
    },
    {
      title: t('pages.admins.actions'),
      width: 88,
      render: (_, row) => {
        const items = rowActions(row);
        if (!items?.length) return null;
        return (
          <Dropdown menu={{ items }} trigger={['click']}>
            <Button
              type="text"
              icon={<MoreOutlined aria-hidden />}
              aria-label={t('pages.admins.actions')}
            />
          </Dropdown>
        );
      },
    },
  ];

  const pagination: TablePaginationConfig = {
    current: page,
    pageSize,
    total: filtered.length,
    showSizeChanger: true,
    onChange: (p, ps) => {
      setPage(p);
      setPageSize(ps);
    },
  };

  const loading = adminsQuery.isFetching;
  const fetched = adminsQuery.data !== undefined || adminsQuery.isError;

  const bulkMenu: MenuProps['items'] = [
    canUpdate && {
      key: 'bulk-disable',
      label: t('pages.admins.disable'),
      onClick: () => runBulk((id) => `/panel/api/admins/disable/${id}`),
    },
    canUpdate && {
      key: 'bulk-enable',
      label: t('pages.admins.enable'),
      onClick: () => runBulk((id) => `/panel/api/admins/enable/${id}`),
    },
    canResetUsage && {
      key: 'bulk-reset',
      label: t('pages.admins.resetUsage'),
      onClick: () => runBulk((id) => `/panel/api/admins/resetUsage/${id}`),
    },
    canUpdate && {
      key: 'bulk-disable-clients',
      label: t('pages.admins.disableAllClients'),
      onClick: () => runBulk((id) => `/panel/api/admins/users/disableActive/${id}`),
    },
    canUpdate && {
      key: 'bulk-activate-clients',
      label: t('pages.admins.activateQuotaDisabledClients'),
      onClick: () => runBulk((id) => `/panel/api/admins/users/activateDisabled/${id}`),
    },
    canUpdate && {
      key: 'bulk-remove-clients',
      label: t('pages.admins.removeAllClients'),
      onClick: () => runBulk((id) => `/panel/api/admins/users/removeAll/${id}`),
    },
    canDelete && {
      key: 'bulk-delete',
      danger: true,
      label: t('pages.admins.delete'),
      onClick: () => runBulk((id) => `/panel/api/admins/del/${id}`),
    },
  ].filter(Boolean) as MenuProps['items'];

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      {modalContextHolder}
      <Layout style={{ minHeight: '100vh' }}>
        <AppSidebar />
        <Layout.Content style={{ padding: isMobile ? 12 : 24 }}>
          {!fetched && loading ? (
            <div style={{ textAlign: 'center', padding: 48 }}>
              <Spin size="large" />
            </div>
          ) : adminsQuery.isError ? (
            <Result status="error" title={t('pages.admins.loadError')} />
          ) : (
            <>
              <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
                <Col xs={12} sm={6}>
                  <Card size="small">
                    <Statistic
                      title={t('pages.admins.statsTotal')}
                      value={stats?.total ?? admins?.length ?? 0}
                      prefix={<UserOutlined aria-hidden />}
                    />
                  </Card>
                </Col>
                <Col xs={12} sm={6}>
                  <Card size="small">
                    <Statistic
                      title={t('pages.admins.statsActive')}
                      value={stats?.active ?? 0}
                      valueStyle={{ color: isDark ? '#73d13d' : '#389e0d' }}
                    />
                  </Card>
                </Col>
                <Col xs={12} sm={6}>
                  <Card size="small">
                    <Statistic
                      title={t('pages.admins.statsDisabled')}
                      value={stats?.disabled ?? 0}
                    />
                  </Card>
                </Col>
                <Col xs={12} sm={6}>
                  <Card size="small">
                    <Statistic title={t('pages.admins.statsLimited')} value={stats?.limited ?? 0} />
                  </Card>
                </Col>
              </Row>

              <Card
                title={t('menu.admins')}
                extra={
                  <Space wrap>
                    <Input.Search
                      allowClear
                      aria-label={t('pages.admins.searchPlaceholder')}
                      placeholder={t('pages.admins.searchPlaceholder')}
                      style={{ width: isMobile ? 160 : 240 }}
                      value={search}
                      onChange={(e) => {
                        setSearch(e.target.value);
                        setPage(1);
                      }}
                    />
                    {selectedIds.length > 0 && (bulkMenu?.length ?? 0) > 0 && (
                      <Dropdown menu={{ items: bulkMenu }}>
                        <Button>
                          {t('pages.admins.bulkActions', { count: selectedIds.length })}
                        </Button>
                      </Dropdown>
                    )}
                    {canCreate && (
                      <Button
                        type="primary"
                        icon={<PlusOutlined aria-hidden />}
                        onClick={openCreate}
                      >
                        {t('pages.admins.add')}
                      </Button>
                    )}
                  </Space>
                }
              >
                <Table<AdminRecord>
                  rowKey="id"
                  loading={loading}
                  columns={columns}
                  dataSource={paged}
                  pagination={pagination}
                  rowSelection={{
                    selectedRowKeys: selectedIds,
                    onChange: (keys) => setSelectedIds(keys.map(Number)),
                    getCheckboxProps: (row) => ({ disabled: row.roleSlug === ownerSlug }),
                  }}
                  scroll={{ x: 800 }}
                  size={isMobile ? 'small' : 'middle'}
                />
              </Card>
            </>
          )}

          <Modal
            title={
              editTarget
                ? t('pages.admins.editTitle', { name: editTarget.username })
                : t('pages.admins.add')
            }
            open={formOpen}
            onCancel={() => {
              setFormOpen(false);
              setEditTarget(null);
            }}
            onOk={() => void submitForm()}
            confirmLoading={saveMut.isPending}
            destroyOnHidden
            width={560}
          >
            <Form form={form} layout="vertical" disabled={!canCreate && !canUpdate}>
              <Form.Item
                name="username"
                label={t('pages.admins.username')}
                rules={[{ required: true }]}
              >
                <Input autoComplete="off" />
              </Form.Item>
              <Form.Item
                name="password"
                label={editTarget ? t('pages.admins.passwordOptional') : t('pages.admins.password')}
                rules={editTarget ? [] : [{ required: true }]}
              >
                <Input.Password autoComplete="new-password" />
              </Form.Item>
              <Form.Item name="roleId" label={t('pages.admins.role')} rules={[{ required: true }]}>
                <Select
                  options={roleOptions.map((r) => ({
                    value: r.id,
                    label: `${r.name} (${r.slug})`,
                  }))}
                  loading={rolesQuery.isFetching}
                />
              </Form.Item>
              <Form.Item
                name="status"
                label={t('pages.admins.status')}
                rules={[{ required: true }]}
              >
                <Select
                  options={[
                    { value: 'active', label: t('pages.admins.active') },
                    { value: 'disabled', label: t('pages.admins.disabled') },
                  ]}
                />
              </Form.Item>
              <Form.Item name="dataLimitGb" label={t('pages.admins.dataLimitGb')}>
                <InputNumber
                  min={0}
                  step={0.5}
                  style={{ width: '100%' }}
                  placeholder={t('pages.admins.unlimitedQuota')}
                />
              </Form.Item>
              <Card size="small" title={t('pages.admins.overridesTitle')} type="inner">
                <Form.Item name="maxUsers" label={t('pages.admins.maxUsers')}>
                  <InputNumber min={0} style={{ width: '100%' }} />
                </Form.Item>
                <Row gutter={12}>
                  <Col span={12}>
                    <Form.Item name="dataLimitMinGb" label={t('pages.admins.dataLimitMinGb')}>
                      <InputNumber min={0} step={0.5} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item name="dataLimitMaxGb" label={t('pages.admins.dataLimitMaxGb')}>
                      <InputNumber min={0} step={0.5} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                </Row>
                <Row gutter={12}>
                  <Col span={12}>
                    <Form.Item name="expireDaysMin" label={t('pages.admins.expireDaysMin')}>
                      <InputNumber min={0} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item name="expireDaysMax" label={t('pages.admins.expireDaysMax')}>
                      <InputNumber min={0} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                </Row>
              </Card>
            </Form>
          </Modal>
        </Layout.Content>
      </Layout>
    </ConfigProvider>
  );
}
