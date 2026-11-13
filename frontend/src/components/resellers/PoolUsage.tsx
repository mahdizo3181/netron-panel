import { useTranslation } from 'react-i18next';
import { Alert, Descriptions, Progress } from 'antd';

import { SizeFormatter } from '@/utils';

export type ResellerState = 'ok' | 'pool' | 'expired' | 'disabled';

export interface PoolUsageProps {
  poolBytes: number;
  usedTraffic: number;
  state?: string;
  // Hides the state banner, e.g. inside a table row that shows a status tag.
  compact?: boolean;
}

// usedTraffic is the server's pool counter: it only grows with billed traffic and
// drops only by an admin action, so it is never recomputed from client rows here.
export function poolPercent(poolBytes: number, usedTraffic: number): number {
  if (poolBytes <= 0) return 0;
  return Math.min(100, Math.round((usedTraffic / poolBytes) * 1000) / 10);
}

export function ResellerStateAlert({ state }: { state?: string }) {
  const { t } = useTranslation();
  if (state !== 'pool' && state !== 'expired' && state !== 'disabled') return null;
  const key = { pool: 'Pool', expired: 'Expired', disabled: 'Disabled' }[state];
  return (
    <Alert
      type={state === 'disabled' ? 'warning' : 'error'}
      showIcon
      title={t(`pages.resellers.state${key}`)}
      description={t(`pages.resellers.state${key}Desc`)}
    />
  );
}

export default function PoolUsage({ poolBytes, usedTraffic, state, compact }: PoolUsageProps) {
  const { t } = useTranslation();
  const limited = poolBytes > 0;
  const percent = poolPercent(poolBytes, usedTraffic);
  const remaining = Math.max(poolBytes - usedTraffic, 0);
  return (
    <div className="pool-usage">
      {!compact && <ResellerStateAlert state={state} />}
      <Progress
        percent={limited ? percent : 0}
        status={limited && usedTraffic >= poolBytes ? 'exception' : 'normal'}
        format={() => (limited ? `${percent}%` : t('unlimited'))}
        aria-label={t('pages.resellers.poolUsed')}
      />
      <Descriptions size="small" column={compact ? 1 : { xs: 1, sm: 3 }}>
        <Descriptions.Item label={t('pages.resellers.poolTotal')}>
          {limited ? SizeFormatter.sizeFormat(poolBytes) : t('unlimited')}
        </Descriptions.Item>
        <Descriptions.Item label={t('pages.resellers.poolUsed')}>
          {SizeFormatter.sizeFormat(usedTraffic)}
        </Descriptions.Item>
        <Descriptions.Item label={t('pages.resellers.poolRemaining')}>
          {limited ? SizeFormatter.sizeFormat(remaining) : t('unlimited')}
        </Descriptions.Item>
      </Descriptions>
    </div>
  );
}
