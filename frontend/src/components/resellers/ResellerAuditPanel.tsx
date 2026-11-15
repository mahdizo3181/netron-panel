import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { DatePicker, Input, Select, Space, Table } from 'antd';
import type { TableColumnsType } from 'antd';
import type { Dayjs } from 'dayjs';

import { useResellerAuditList } from '@/api/queries/useResellers';
import { IntlUtil } from '@/utils';
import type { ResellerAuditLog, ResellerView } from '@/generated/types';

const ACTION_OPTIONS = [
  'create',
  'bulkCreate',
  'delete',
  'bulkEnable',
  'bulkDisable',
  'enableClients',
  'disableClients',
  'resetTraffic',
  'cutoff',
  'release',
  'setOwner',
  'update',
  'addVolume',
  'resetUsage',
  'extendExpiry',
  'setPassword',
  'delete',
];

interface Props {
  resellerId?: number;
  self?: boolean;
  resellers?: ResellerView[];
}

export default function ResellerAuditPanel({ resellerId, self, resellers }: Props) {
  const { t } = useTranslation();
  const filterId = useId();
  const [page, setPage] = useState(1);
  const [action, setAction] = useState<string | undefined>();
  const [range, setRange] = useState<[Dayjs | null, Dayjs | null] | null>(null);
  const [pickedReseller, setPickedReseller] = useState<number | undefined>(resellerId);

  const query = useResellerAuditList({
    resellerId: self ? undefined : (pickedReseller ?? resellerId),
    self,
    page,
    pageSize: 20,
    action,
    from: range?.[0]?.valueOf(),
    to: range?.[1]?.valueOf(),
  });

  const columns: TableColumnsType<ResellerAuditLog> = [
    {
      title: t('pages.resellers.audit.at'),
      dataIndex: 'at',
      width: 160,
      render: (at: number) => IntlUtil.formatDate(at),
    },
    { title: t('pages.resellers.audit.action'), dataIndex: 'action', width: 120 },
    { title: t('pages.resellers.audit.target'), dataIndex: 'target', ellipsis: true },
    { title: t('pages.resellers.audit.detail'), dataIndex: 'detail', ellipsis: true },
    {
      title: t('pages.resellers.audit.actor'),
      key: 'actor',
      width: 100,
      render: (_, r) => `${r.actorKind}${r.actorId ? ` #${r.actorId}` : ''}`,
    },
  ];

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Space wrap>
        {!self && !resellerId && (
          <Select
            allowClear
            placeholder={t('pages.resellers.audit.resellerFilter')}
            aria-label={t('pages.resellers.audit.resellerFilter')}
            style={{ minWidth: 200 }}
            value={pickedReseller}
            onChange={(v) => {
              setPickedReseller(v);
              setPage(1);
            }}
            options={(resellers ?? []).map((r) => ({ value: r.id, label: r.name || r.username }))}
          />
        )}
        <Select
          allowClear
          placeholder={t('pages.resellers.audit.actionFilter')}
          aria-label={t('pages.resellers.audit.actionFilter')}
          style={{ minWidth: 160 }}
          value={action}
          onChange={(v) => {
            setAction(v);
            setPage(1);
          }}
          options={ACTION_OPTIONS.map((a) => ({ value: a, label: a }))}
        />
        <DatePicker.RangePicker
          id={`${filterId}-range`}
          aria-label={t('pages.resellers.audit.at')}
          value={range ?? undefined}
          onChange={(v) => {
            setRange(v);
            setPage(1);
          }}
        />
        <Input.Search
          placeholder={t('refresh')}
          aria-label={t('refresh')}
          enterButton={t('refresh')}
          style={{ width: 120 }}
          onSearch={() => void query.refetch()}
        />
      </Space>
      <Table<ResellerAuditLog>
        rowKey="id"
        size="small"
        loading={query.isPending}
        columns={columns}
        dataSource={query.data?.rows ?? []}
        pagination={{
          current: page,
          pageSize: 20,
          total: query.data?.total ?? 0,
          showSizeChanger: false,
          onChange: setPage,
        }}
      />
    </Space>
  );
}
