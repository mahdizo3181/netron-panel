import { useTranslation } from 'react-i18next';
import { Button, InputNumber, Space, Typography } from 'antd';
import { useFormContext, useWatch } from 'react-hook-form';

import { FormField, rhfZodValidate } from '@/components/form/rhf';
import {
  USAGE_MULTIPLIER_CHIPS,
  effectiveUsageMultiplier,
  formatUsageMultiplier,
  supportsUsageMultiplier,
} from '@/lib/xray/usage-multiplier';
import { InboundDbFieldsSchema } from '@/schemas/forms/inbound-form';

export default function UsageMultiplierField({ protocol }: { protocol: string }) {
  const { t } = useTranslation();
  const { setValue } = useFormContext();
  const current = effectiveUsageMultiplier(useWatch({ name: 'usageMultiplier' }) as number);
  const supported = supportsUsageMultiplier(protocol);

  return (
    <FormField
      name="usageMultiplier"
      label={t('pages.inbounds.form.usageMultiplier')}
      tooltip={t('pages.inbounds.form.usageMultiplierHelp')}
      rules={{ validate: rhfZodValidate(InboundDbFieldsSchema.shape.usageMultiplier) }}
      extra={
        supported ? (
          <>
            <Space size={4} wrap style={{ marginBottom: 4 }}>
              {USAGE_MULTIPLIER_CHIPS.map((chip) => (
                <Button
                  key={chip}
                  size="small"
                  type={current === chip ? 'primary' : 'default'}
                  onClick={() => setValue('usageMultiplier', chip, { shouldDirty: true })}
                >
                  {formatUsageMultiplier(chip)}
                </Button>
              ))}
            </Space>
            <br />
            <Typography.Text type="secondary">
              {t('pages.inbounds.form.usageMultiplierFutureOnly')}
            </Typography.Text>
          </>
        ) : (
          <Typography.Text type="secondary">
            {t('pages.inbounds.form.usageMultiplierUnsupported')}
          </Typography.Text>
        )
      }
    >
      <InputNumber min={0} max={10} step={0.1} disabled={!supported} />
    </FormField>
  );
}
