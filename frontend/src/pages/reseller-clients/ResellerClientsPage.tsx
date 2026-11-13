import { useEffect, useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  ConfigProvider,
  Form,
  Input,
  InputNumber,
  Layout,
  Modal,
  Popconfirm,
  QRCode,
  Select,
  Space,
  Switch,
  Table,
  Tooltip,
  Typography,
  message,
} from 'antd';
import type { TableColumnsType } from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  QrcodeOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import dayjs from 'dayjs';

import AppSidebar from '@/layouts/AppSidebar';
import { DateTimePicker } from '@/components/form';
import { keys } from '@/api/queryKeys';
import { useResellerMe, useResellerSubSettings } from '@/api/queries/useResellers';
import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { HttpUtil, IntlUtil, SizeFormatter } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import type {
  ClientPageResponse,
  ClientSlim,
  InboundOption,
  ResellerSelfView,
} from '@/generated/types';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };
const PAGE_SIZE = 25;
const POLL_MS = 15_000;

// Why adding or enabling is refused now, or '' when it is allowed; the server
// enforces the same rules, this only explains a disabled button.
export function addBlockedReason(
  r: ResellerSelfView | null | undefined,
  forEnable = false,
): string {
  if (!r) return 'loading';
  if (r.state === 'pool') return 'pages.resellers.statePool';
  if (r.state === 'expired') return 'pages.resellers.stateExpired';
  if (r.state === 'disabled') return 'pages.resellers.stateDisabled';
  if (!forEnable && r.maxClients > 0 && r.clientCount >= r.maxClients)
    return 'pages.resellerClients.limitReached';
  return '';
}

interface FormState {
  email: string;
  inboundIds: number[];
  totalGB: number;
  expiryTime: number;
  comment: string;
  enable: boolean;
}

const emptyForm: FormState = {
  email: '',
  inboundIds: [],
  totalGB: 0,
  expiryTime: 0,
  comment: '',
  enable: true,
};

export default function ResellerClientsPage() {
  usePageTitle();
  const { t } = useTranslation();
  const uid = useId();
  const { antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => setMessageInstance(messageApi), [messageApi]);
  const queryClient = useQueryClient();
  const me = useResellerMe();
  const settings = useResellerSubSettings();
  const r = me.data?.reseller;
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState('');
  const [editing, setEditing] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [qrFor, setQrFor] = useState<ClientSlim | null>(null);

  const listKey = [...keys.clients.root(), 'reseller', page, search];
  const list = useQuery({
    queryKey: listKey,
    refetchInterval: POLL_MS,
    queryFn: async (): Promise<ClientPageResponse> => {
      const qs = new URLSearchParams({
        page: String(page),
        pageSize: String(PAGE_SIZE),
        sort: 'email',
        order: 'asc',
      });
      if (search) qs.set('search', search);
      const msg = await HttpUtil.get<ClientPageResponse>(
        `/panel/api/clients/list/paged?${qs}`,
        undefined,
        { silent: true },
      );
      if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Failed to fetch clients');
      return msg.obj;
    },
  });
  const options = useQuery({
    queryKey: [...keys.clients.root(), 'reseller-options'],
    queryFn: async (): Promise<InboundOption[]> => {
      const msg = await HttpUtil.get<InboundOption[]>('/panel/api/inbounds/options', undefined, {
        silent: true,
      });
      return (msg?.success && msg.obj) || [];
    },
  });
  const inboundName = useMemo(() => {
    const m = new Map<number, string>();
    for (const o of options.data ?? []) m.set(o.id, o.remark || `${o.protocol}:${o.port}`);
    return m;
  }, [options.data]);

  // Pool numbers come from /reseller/me again after every change, never from these rows.
  const changed = () => {
    void queryClient.invalidateQueries({ queryKey: keys.clients.root() });
    void queryClient.invalidateQueries({ queryKey: keys.resellers.me() });
  };
  const run = async (call: Promise<{ success?: boolean } | undefined>) => {
    const msg = await call;
    if (msg?.success) changed();
    return msg;
  };

  const addReason = addBlockedReason(r);
  const enableReason = addBlockedReason(r, true);

  async function openEdit(email: string) {
    const msg = await HttpUtil.get<{ client: Record<string, unknown>; inboundIds: number[] }>(
      `/panel/api/clients/get/${encodeURIComponent(email)}`,
    );
    if (!msg?.success || !msg.obj) return;
    const c = msg.obj.client;
    setForm({
      email,
      inboundIds: msg.obj.inboundIds ?? [],
      totalGB: Number(c.totalGB ?? 0) / SizeFormatter.ONE_GB,
      expiryTime: Number(c.expiryTime ?? 0),
      comment: String(c.comment ?? ''),
      enable: Boolean(c.enable),
    });
    setEditing(email);
  }

  async function save() {
    const fields = {
      email: form.email.trim(),
      totalGB: Math.round(form.totalGB * SizeFormatter.ONE_GB),
      expiryTime: form.expiryTime,
      comment: form.comment,
      enable: form.enable,
    };
    let msg;
    if (editing === '') {
      msg = await run(
        HttpUtil.post(
          '/panel/api/clients/add',
          { client: { ...fields, id: crypto.randomUUID() }, inboundIds: form.inboundIds },
          JSON_HEADERS,
        ),
      );
    } else if (editing) {
      // Send the stored client back with the edited fields, so credentials stay as they are.
      const current = await HttpUtil.get<{ client: Record<string, unknown> }>(
        `/panel/api/clients/get/${encodeURIComponent(editing)}`,
      );
      if (!current?.success || !current.obj) return;
      const stored = current.obj.client;
      msg = await run(
        HttpUtil.post(
          `/panel/api/clients/update/${encodeURIComponent(editing)}`,
          { ...stored, id: stored.uuid, ...fields },
          JSON_HEADERS,
        ),
      );
    }
    if (msg?.success) setEditing(null);
  }

  const subLink = (c: ClientSlim) =>
    settings.data?.subEnable && settings.data.subURI && c.subId
      ? settings.data.subURI + c.subId
      : '';

  const columns: TableColumnsType<ClientSlim> = [
    {
      title: t('pages.clients.client'),
      key: 'email',
      render: (_v, c) => (
        <Space direction="vertical" size={0}>
          <span>{c.email}</span>
          {c.comment && <Typography.Text type="secondary">{c.comment}</Typography.Text>}
        </Space>
      ),
    },
    {
      title: t('enable'),
      key: 'enable',
      width: 90,
      render: (_v, c) => {
        const blocked = !c.enable && enableReason !== '';
        return (
          <Tooltip title={blocked ? t(enableReason) : undefined}>
            <Switch
              size="small"
              checked={c.enable}
              disabled={blocked}
              aria-label={`${t('enable')} ${c.email}`}
              onChange={(on) =>
                run(
                  HttpUtil.post(
                    `/panel/api/clients/${on ? 'bulkEnable' : 'bulkDisable'}`,
                    { emails: [c.email] },
                    JSON_HEADERS,
                  ),
                )
              }
            />
          </Tooltip>
        );
      },
    },
    {
      title: t('pages.clients.traffic'),
      key: 'traffic',
      render: (_v, c) => {
        const used = (c.traffic?.up ?? 0) + (c.traffic?.down ?? 0);
        return `${SizeFormatter.sizeFormat(used)} / ${c.totalGB > 0 ? SizeFormatter.sizeFormat(c.totalGB) : t('unlimited')}`;
      },
    },
    {
      title: t('pages.inbounds.expireDate'),
      key: 'expiry',
      render: (_v, c) =>
        c.expiryTime > 0
          ? IntlUtil.formatDate(c.expiryTime, settings.data?.datepicker ?? 'gregorian')
          : t('pages.resellers.never'),
    },
    {
      title: t('pages.resellerClients.inbounds'),
      key: 'inbounds',
      render: (_v, c) =>
        (c.inboundIds ?? []).map((id) => inboundName.get(id) ?? `#${id}`).join(', '),
    },
    {
      title: '',
      key: 'actions',
      width: 170,
      render: (_v, c) => (
        <Space size={4}>
          <Button
            size="small"
            icon={<EditOutlined aria-hidden />}
            aria-label={`${t('edit')} ${c.email}`}
            onClick={() => openEdit(c.email)}
          />
          <Button
            size="small"
            icon={<QrcodeOutlined aria-hidden />}
            aria-label={`${t('qrCode')} ${c.email}`}
            onClick={() => setQrFor(c)}
          />
          <Popconfirm
            title={t('pages.resellerClients.resetConfirm', { email: c.email })}
            onConfirm={() =>
              run(HttpUtil.post(`/panel/api/clients/resetTraffic/${encodeURIComponent(c.email)}`))
            }
            disabled={enableReason !== ''}
          >
            <Tooltip title={enableReason ? t(enableReason) : t('pages.inbounds.resetTraffic')}>
              <Button
                size="small"
                icon={<ReloadOutlined aria-hidden />}
                disabled={enableReason !== ''}
                aria-label={`${t('pages.inbounds.resetTraffic')} ${c.email}`}
              />
            </Tooltip>
          </Popconfirm>
          <Popconfirm
            title={t('pages.resellerClients.deleteConfirm', { email: c.email })}
            onConfirm={() =>
              run(HttpUtil.post(`/panel/api/clients/del/${encodeURIComponent(c.email)}`))
            }
          >
            <Button
              size="small"
              danger
              icon={<DeleteOutlined aria-hidden />}
              aria-label={`${t('delete')} ${c.email}`}
            />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const qrLink = qrFor ? subLink(qrFor) : '';

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className="reseller-clients">
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Card
              title={t('menu.clients')}
              extra={
                <Tooltip title={addReason && addReason !== 'loading' ? t(addReason) : undefined}>
                  <Button
                    type="primary"
                    icon={<PlusOutlined aria-hidden />}
                    disabled={addReason !== ''}
                    onClick={() => {
                      setForm(emptyForm);
                      setEditing('');
                    }}
                  >
                    {t('pages.clients.addClient')}
                  </Button>
                </Tooltip>
              }
            >
              {addReason && addReason !== 'loading' && (
                <Alert
                  type="warning"
                  showIcon
                  className="mb-12"
                  title={t(addReason)}
                  data-testid="add-blocked"
                />
              )}
              <Input.Search
                allowClear
                aria-label={t('pages.clients.email')}
                placeholder={t('pages.clients.email')}
                onSearch={(v) => {
                  setPage(1);
                  setSearch(v);
                }}
                style={{ maxWidth: 320, marginBottom: 12 }}
              />
              <Table<ClientSlim>
                rowKey="email"
                size="small"
                loading={list.isPending}
                columns={columns}
                dataSource={list.data?.items ?? []}
                scroll={{ x: 760 }}
                pagination={{
                  current: page,
                  pageSize: PAGE_SIZE,
                  total: list.data?.filtered ?? 0,
                  onChange: setPage,
                  showSizeChanger: false,
                }}
              />
            </Card>
          </Layout.Content>
        </Layout>
      </Layout>

      <Modal
        open={editing !== null}
        title={editing === '' ? t('pages.clients.addClient') : t('edit')}
        okText={t('save')}
        cancelText={t('cancel')}
        onCancel={() => setEditing(null)}
        onOk={save}
        destroyOnHidden
      >
        <Form layout="vertical">
          <Form.Item label={t('pages.clients.email')} htmlFor={`${uid}-f1`} required>
            <Input
              id={`${uid}-f1`}
              value={form.email}
              disabled={editing !== ''}
              onChange={(e) => setForm({ ...form, email: e.target.value })}
            />
          </Form.Item>
          <Form.Item label={t('pages.resellerClients.inbounds')} htmlFor={`${uid}-f2`} required>
            <Select
              id={`${uid}-f2`}
              mode="multiple"
              disabled={editing !== ''}
              value={form.inboundIds}
              onChange={(v) => setForm({ ...form, inboundIds: v })}
              options={(options.data ?? []).map((o) => ({
                value: o.id,
                label: inboundName.get(o.id),
              }))}
            />
          </Form.Item>
          <Form.Item
            label={t('pages.resellerClients.totalGB')}
            htmlFor={`${uid}-f3`}
            extra={r?.requireClientQuota ? t('pages.resellerClients.quotaRequired') : undefined}
          >
            <InputNumber
              id={`${uid}-f3`}
              min={0}
              step={1}
              value={form.totalGB}
              onChange={(v) => setForm({ ...form, totalGB: Number(v ?? 0) })}
              style={{ width: '100%' }}
            />
          </Form.Item>
          <Form.Item label={t('pages.inbounds.expireDate')} htmlFor={`${uid}-f4`}>
            <DateTimePicker
              id={`${uid}-f4`}
              value={form.expiryTime > 0 ? dayjs(form.expiryTime) : null}
              onChange={(d) => setForm({ ...form, expiryTime: d ? d.valueOf() : 0 })}
            />
          </Form.Item>
          <Form.Item label={t('comment')} htmlFor={`${uid}-f5`}>
            <Input
              id={`${uid}-f5`}
              value={form.comment}
              onChange={(e) => setForm({ ...form, comment: e.target.value })}
            />
          </Form.Item>
          <Form.Item label={t('enable')} htmlFor={`${uid}-f6`}>
            <Switch
              id={`${uid}-f6`}
              checked={form.enable}
              disabled={!form.enable && enableReason !== ''}
              onChange={(v) => setForm({ ...form, enable: v })}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={qrFor !== null}
        title={qrFor?.email}
        footer={null}
        onCancel={() => setQrFor(null)}
        destroyOnHidden
      >
        {qrLink ? (
          <Space direction="vertical" align="center" style={{ width: '100%' }}>
            <QRCode value={qrLink} size={220} />
            <Typography.Text copyable={{ text: qrLink }} style={{ wordBreak: 'break-all' }}>
              {qrLink}
            </Typography.Text>
          </Space>
        ) : (
          <Alert type="info" showIcon title={t('pages.resellerClients.noSubscription')} />
        )}
      </Modal>
    </ConfigProvider>
  );
}
