import { useEffect, useId, useMemo, useState } from 'react';
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
  Radio,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  message,
} from 'antd';
import type { TableColumnsType } from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  KeyOutlined,
  PlusOutlined,
  SafetyOutlined,
} from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import dayjs from 'dayjs';

import AppSidebar from '@/layouts/AppSidebar';
import { DateTimePicker } from '@/components/form';
import PoolUsage from '@/components/resellers/PoolUsage';
import {
  useResellerList,
  useResellerMutations,
  type ResellerFormPayload,
} from '@/api/queries/useResellers';
import { keys } from '@/api/queryKeys';
import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { HttpUtil, IntlUtil, SizeFormatter } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import type { InboundOption, ResellerView } from '@/generated/types';

// Mirrors the backend's rules in service/reseller.go so mistakes show before saving.
export const RESELLER_PROTOCOLS = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria'];
export const RESELLER_USERNAME = /^[A-Za-z0-9._-]{3,32}$/;
export function validResellerPassword(username: string, password: string): boolean {
  return password.length >= 10 && password.toLowerCase() !== username.toLowerCase();
}

export function resellerStateOf(
  r: ResellerView,
  nowMs: number,
): 'ok' | 'pool' | 'expired' | 'disabled' {
  if (!r.enable) return 'disabled';
  if (r.expiryTime > 0 && r.expiryTime <= nowMs) return 'expired';
  if (r.poolBytes > 0 && r.usedTraffic >= r.poolBytes) return 'pool';
  return 'ok';
}

const STATE_COLOR = { ok: 'green', pool: 'red', expired: 'red', disabled: 'default' } as const;
const STATE_KEY = { ok: 'Ok', pool: 'Pool', expired: 'Expired', disabled: 'Disabled' } as const;

interface FormState {
  username: string;
  password: string;
  name: string;
  enable: boolean;
  poolGB: number;
  maxClients: number;
  expiryTime: number;
  requireClientQuota: boolean;
  inboundIds: number[];
}

const emptyForm: FormState = {
  username: '',
  password: '',
  name: '',
  enable: true,
  poolGB: 0,
  maxClients: 0,
  expiryTime: 0,
  requireClientQuota: false,
  inboundIds: [],
};

export default function ResellersPage() {
  usePageTitle();
  const { t } = useTranslation();
  const uid = useId();
  const { antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  const [modal, modalContextHolder] = Modal.useModal();
  useEffect(() => setMessageInstance(messageApi), [messageApi]);
  const list = useResellerList();
  const { create, update, setPassword, remove, resetTotp } = useResellerMutations();
  const [editing, setEditing] = useState<number | 'new' | null>(null);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [pwFor, setPwFor] = useState<ResellerView | null>(null);
  const [newPassword, setNewPassword] = useState('');
  const [delFor, setDelFor] = useState<ResellerView | null>(null);
  const [delMode, setDelMode] = useState<'transfer' | 'delete'>('transfer');

  const options = useQuery({
    queryKey: [...keys.resellers.root(), 'inbound-options'],
    queryFn: async (): Promise<InboundOption[]> => {
      const msg = await HttpUtil.get<InboundOption[]>('/panel/api/inbounds/options', undefined, {
        silent: true,
      });
      return (msg?.success && msg.obj) || [];
    },
  });
  const inboundChoices = useMemo(
    () =>
      (options.data ?? [])
        .filter((o) => RESELLER_PROTOCOLS.includes(o.protocol))
        .map((o) => ({ value: o.id, label: `${o.remark || o.tag} (${o.protocol}:${o.port})` })),
    [options.data],
  );

  const isNew = editing === 'new';
  const formErrors = {
    username:
      isNew && !RESELLER_USERNAME.test(form.username) ? t('pages.resellers.usernameRule') : '',
    password:
      isNew && !validResellerPassword(form.username, form.password)
        ? t('pages.resellers.passwordRule')
        : '',
  };
  const formValid =
    !formErrors.username && !formErrors.password && form.poolGB >= 0 && form.maxClients >= 0;

  function openEdit(r: ResellerView) {
    setForm({
      username: r.username,
      password: '',
      name: r.name,
      enable: r.enable,
      poolGB: r.poolBytes / SizeFormatter.ONE_GB,
      maxClients: r.maxClients,
      expiryTime: r.expiryTime,
      requireClientQuota: r.requireClientQuota,
      inboundIds: r.inboundIds ?? [],
    });
    setEditing(r.id);
  }

  async function save() {
    const p: ResellerFormPayload = {
      name: form.name,
      enable: form.enable,
      poolBytes: Math.round(form.poolGB * SizeFormatter.ONE_GB),
      maxClients: form.maxClients,
      expiryTime: form.expiryTime,
      requireClientQuota: form.requireClientQuota,
      inboundIds: form.inboundIds,
    };
    const msg =
      editing === 'new'
        ? await create.mutateAsync({ ...p, username: form.username, password: form.password })
        : await update.mutateAsync({ id: editing as number, p });
    if (msg?.success) setEditing(null);
  }

  // When the list was fetched: a pure clock for the expired check.
  const now = list.dataUpdatedAt;
  const columns: TableColumnsType<ResellerView> = [
    {
      title: t('pages.resellers.name'),
      key: 'name',
      render: (_v, r) => (
        <Space direction="vertical" size={0}>
          <strong>{r.name || r.username}</strong>
          <span className="sub">{r.username}</span>
        </Space>
      ),
    },
    {
      title: t('status'),
      key: 'state',
      width: 130,
      render: (_v, r) => {
        const state = resellerStateOf(r, now);
        return (
          <Tag color={STATE_COLOR[state]}>{t(`pages.resellers.state${STATE_KEY[state]}`)}</Tag>
        );
      },
    },
    {
      title: t('pages.resellers.poolTotal'),
      key: 'pool',
      width: 260,
      render: (_v, r) => <PoolUsage poolBytes={r.poolBytes} usedTraffic={r.usedTraffic} compact />,
    },
    {
      title: t('pages.resellers.clients'),
      key: 'clients',
      width: 110,
      render: (_v, r) => (r.maxClients > 0 ? `${r.clientCount} / ${r.maxClients}` : r.clientCount),
    },
    {
      title: t('pages.resellers.expiry'),
      key: 'expiry',
      render: (_v, r) =>
        r.expiryTime > 0 ? IntlUtil.formatDate(r.expiryTime) : t('pages.resellers.never'),
    },
    {
      title: '',
      key: 'actions',
      width: 170,
      render: (_v, r) => (
        <Space size={4}>
          <Button
            size="small"
            icon={<EditOutlined aria-hidden />}
            aria-label={`${t('edit')} ${r.username}`}
            onClick={() => openEdit(r)}
          />
          <Button
            size="small"
            icon={<KeyOutlined aria-hidden />}
            aria-label={`${t('pages.resellers.setPassword')} ${r.username}`}
            onClick={() => setPwFor(r)}
          />
          <Button
            size="small"
            icon={<SafetyOutlined aria-hidden />}
            aria-label={`${t('pages.resellers.resetTotp')} ${r.username}`}
            onClick={() =>
              modal.confirm({
                title: t('pages.resellers.resetTotp'),
                okText: t('confirm'),
                cancelText: t('cancel'),
                onOk: () => resetTotp.mutateAsync(r.id),
              })
            }
          />
          <Button
            size="small"
            danger
            icon={<DeleteOutlined aria-hidden />}
            aria-label={`${t('delete')} ${r.username}`}
            onClick={() => {
              setDelMode('transfer');
              setDelFor(r);
            }}
          />
        </Space>
      ),
    },
  ];

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      {modalContextHolder}
      <Layout className="resellers-page">
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Card
              title={t('menu.resellers')}
              extra={
                <Button
                  type="primary"
                  icon={<PlusOutlined aria-hidden />}
                  onClick={() => {
                    setForm(emptyForm);
                    setEditing('new');
                  }}
                >
                  {t('pages.resellers.add')}
                </Button>
              }
            >
              <Table<ResellerView>
                rowKey="id"
                size="small"
                loading={list.isPending}
                columns={columns}
                dataSource={list.data ?? []}
                scroll={{ x: 900 }}
                pagination={false}
              />
            </Card>
          </Layout.Content>
        </Layout>
      </Layout>

      <Modal
        open={editing !== null}
        title={isNew ? t('pages.resellers.add') : t('edit')}
        okText={t('save')}
        cancelText={t('cancel')}
        okButtonProps={{ disabled: !formValid }}
        confirmLoading={create.isPending || update.isPending}
        onCancel={() => setEditing(null)}
        onOk={save}
        destroyOnHidden
      >
        <Form layout="vertical">
          <Form.Item
            label={t('username')}
            htmlFor={`${uid}-f1`}
            required
            validateStatus={formErrors.username ? 'error' : undefined}
            help={formErrors.username || undefined}
          >
            <Input
              id={`${uid}-f1`}
              value={form.username}
              disabled={!isNew}
              autoComplete="off"
              onChange={(e) => setForm({ ...form, username: e.target.value })}
            />
          </Form.Item>
          {isNew && (
            <Form.Item
              label={t('password')}
              htmlFor={`${uid}-f2`}
              required
              validateStatus={formErrors.password ? 'error' : undefined}
              help={formErrors.password || undefined}
            >
              <Input.Password
                id={`${uid}-f2`}
                value={form.password}
                autoComplete="new-password"
                onChange={(e) => setForm({ ...form, password: e.target.value })}
              />
            </Form.Item>
          )}
          <Form.Item label={t('pages.resellers.name')} htmlFor={`${uid}-f3`}>
            <Input
              id={`${uid}-f3`}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </Form.Item>
          <Form.Item label={t('pages.resellers.poolGB')} htmlFor={`${uid}-f4`}>
            <InputNumber
              id={`${uid}-f4`}
              min={0}
              value={form.poolGB}
              onChange={(v) => setForm({ ...form, poolGB: Number(v ?? 0) })}
              style={{ width: '100%' }}
            />
          </Form.Item>
          <Form.Item label={t('pages.resellers.maxClients')} htmlFor={`${uid}-f5`}>
            <InputNumber
              id={`${uid}-f5`}
              min={0}
              precision={0}
              value={form.maxClients}
              onChange={(v) => setForm({ ...form, maxClients: Number(v ?? 0) })}
              style={{ width: '100%' }}
            />
          </Form.Item>
          <Form.Item label={t('pages.resellers.expiry')} htmlFor={`${uid}-f6`}>
            <DateTimePicker
              id={`${uid}-f6`}
              value={form.expiryTime > 0 ? dayjs(form.expiryTime) : null}
              onChange={(d) => setForm({ ...form, expiryTime: d ? d.valueOf() : 0 })}
            />
          </Form.Item>
          <Form.Item
            label={t('pages.resellers.allowedInbounds')}
            htmlFor={`${uid}-f7`}
            extra={t('pages.resellers.allowedInboundsHelp')}
          >
            <Select
              id={`${uid}-f7`}
              mode="multiple"
              value={form.inboundIds}
              options={inboundChoices}
              onChange={(v) => setForm({ ...form, inboundIds: v })}
            />
          </Form.Item>
          <Form.Item>
            <Checkbox
              checked={form.requireClientQuota}
              onChange={(e) => setForm({ ...form, requireClientQuota: e.target.checked })}
            >
              {t('pages.resellers.requireClientQuota')}
            </Checkbox>
          </Form.Item>
          <Form.Item label={t('enable')} htmlFor={`${uid}-f8`}>
            <Switch
              id={`${uid}-f8`}
              checked={form.enable}
              onChange={(v) => setForm({ ...form, enable: v })}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={pwFor !== null}
        title={`${t('pages.resellers.setPassword')}: ${pwFor?.username ?? ''}`}
        okText={t('save')}
        cancelText={t('cancel')}
        okButtonProps={{ disabled: !pwFor || !validResellerPassword(pwFor.username, newPassword) }}
        onCancel={() => setPwFor(null)}
        onOk={async () => {
          const msg = await setPassword.mutateAsync({ id: pwFor!.id, password: newPassword });
          if (msg?.success) {
            setPwFor(null);
            setNewPassword('');
          }
        }}
        destroyOnHidden
      >
        <Form layout="vertical">
          <Form.Item
            label={t('pages.resellers.newPassword')}
            htmlFor={`${uid}-f9`}
            extra={t('pages.resellers.passwordRule')}
          >
            <Input.Password
              id={`${uid}-f9`}
              value={newPassword}
              autoComplete="new-password"
              onChange={(e) => setNewPassword(e.target.value)}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={delFor !== null}
        title={t('pages.resellers.deleteTitle', { name: delFor?.name || delFor?.username || '' })}
        okText={t('delete')}
        okButtonProps={{ danger: true }}
        cancelText={t('cancel')}
        onCancel={() => setDelFor(null)}
        onOk={async () => {
          const msg = await remove.mutateAsync({ id: delFor!.id, clients: delMode });
          if (msg?.success) setDelFor(null);
        }}
        destroyOnHidden
      >
        <Radio.Group value={delMode} onChange={(e) => setDelMode(e.target.value)}>
          <Space direction="vertical">
            <Radio value="transfer">{t('pages.resellers.deleteTransfer')}</Radio>
            <Radio value="delete">{t('pages.resellers.deleteClients')}</Radio>
          </Space>
        </Radio.Group>
      </Modal>
    </ConfigProvider>
  );
}
