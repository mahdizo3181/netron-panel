import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Popconfirm, Switch, Typography } from 'antd';

import { HttpUtil } from '@/utils';
import { SettingListItem } from '@/components/ui';
import { useUsageMultiplierStatus } from '@/api/queries/useUsageMultiplierStatus';
import type { AllSetting } from '@/models/setting';

interface Props {
  allSetting: AllSetting;
  updateSetting: (patch: Partial<AllSetting>) => void;
}

export default function UsageMultiplierSetting({ allSetting, updateSetting }: Props) {
  const { t } = useTranslation();
  const { data: status, refetch } = useUsageMultiplierStatus();
  const [restarting, setRestarting] = useState(false);

  const onOff = (on: boolean) =>
    t(on ? 'pages.settings.usageMultiplierOn' : 'pages.settings.usageMultiplierOff');
  const pending = status != null && status.configured !== status.active;
  const staleNodes = (status?.nodes ?? []).filter((n) => n.known && !n.applies);
  const unknownNodes = (status?.nodes ?? []).filter((n) => !n.known);

  async function restartXray() {
    setRestarting(true);
    try {
      await HttpUtil.post('/panel/api/server/restartXrayService');
      await refetch();
    } finally {
      setRestarting(false);
    }
  }

  return (
    <>
      <SettingListItem
        paddings="small"
        title={t('pages.settings.usageMultiplierEnable')}
        description={t('pages.settings.usageMultiplierEnableDesc')}
      >
        <Switch
          checked={allSetting.usageMultiplierEnable}
          onChange={(v) => updateSetting({ usageMultiplierEnable: v })}
        />
      </SettingListItem>

      <div style={{ padding: '8px 20px 0' }}>
        {status && (
          <Typography.Paragraph type="secondary" style={{ margin: '0 0 8px' }}>
            {t('pages.settings.usageMultiplierStatus', {
              saved: onOff(status.configured),
              running: onOff(status.active),
            })}
          </Typography.Paragraph>
        )}

        {pending && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 8 }}
            title={t('pages.settings.usageMultiplierPending')}
            description={t('pages.settings.usageMultiplierPendingDesc')}
            action={
              <Popconfirm
                title={t('pages.settings.usageMultiplierRestartConfirm')}
                onConfirm={restartXray}
              >
                <Button size="small" danger loading={restarting}>
                  {t('pages.settings.usageMultiplierRestart')}
                </Button>
              </Popconfirm>
            }
          />
        )}

        {staleNodes.length > 0 && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 8 }}
            title={t('pages.settings.usageMultiplierNodesTitle')}
            description={
              <ul style={{ margin: 0, paddingInlineStart: 20 }}>
                {staleNodes.map((n) => (
                  <li key={n.nodeId}>
                    {t('pages.settings.usageMultiplierNodeWarn', {
                      name: n.name,
                      ids: n.inboundIds.map((id) => `#${id}`).join(', '),
                    })}
                  </li>
                ))}
              </ul>
            }
          />
        )}

        {unknownNodes.length > 0 && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 8 }}
            title={t('pages.settings.usageMultiplierNodeUnknown', {
              names: unknownNodes.map((n) => n.name).join(', '),
            })}
          />
        )}
      </div>
    </>
  );
}
