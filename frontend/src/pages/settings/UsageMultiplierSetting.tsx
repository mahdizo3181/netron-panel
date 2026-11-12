import { useTranslation } from 'react-i18next';
import { Alert, Switch, Typography } from 'antd';

import { SettingListItem } from '@/components/ui';
import { useUsageMultiplierStatus } from '@/api/queries/useUsageMultiplierStatus';
import type { AllSetting } from '@/models/setting';

interface Props {
  allSetting: AllSetting;
  updateSetting: (patch: Partial<AllSetting>) => void;
}

export default function UsageMultiplierSetting({ allSetting, updateSetting }: Props) {
  const { t } = useTranslation();
  const { data: status } = useUsageMultiplierStatus();

  const onOff = (on: boolean) =>
    t(on ? 'pages.settings.usageMultiplierOn' : 'pages.settings.usageMultiplierOff');
  const staleNodes = (status?.nodes ?? []).filter((n) => n.known && !n.applies);
  const unknownNodes = (status?.nodes ?? []).filter((n) => !n.known);

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
