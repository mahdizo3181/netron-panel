import { useTranslation } from 'react-i18next';
import { InputNumber, Typography } from 'antd';

import { FormField, rhfZodValidate } from '@/components/form/rhf';
import { useSavedUsageMultiplierEnable } from '@/api/queries/useSavedUsageMultiplierEnable';
import { supportsUsageMultiplier } from '@/lib/xray/usage-multiplier';
import { InboundDbFieldsSchema } from '@/schemas/forms/inbound-form';

export default function UsageMultiplierField({ protocol }: { protocol: string }) {
  const { t } = useTranslation();
  const supported = supportsUsageMultiplier(protocol);
  const savedOn = useSavedUsageMultiplierEnable();
  // Locked until the saved switch reads ON; the stored value is still shown and submitted.
  const editable = supported && savedOn === true;

  return (
    <FormField
      name="usageMultiplier"
      label={t('pages.inbounds.form.usageMultiplier')}
      tooltip={t('pages.inbounds.form.usageMultiplierHelp')}
      rules={{ validate: rhfZodValidate(InboundDbFieldsSchema.shape.usageMultiplier) }}
      extra={
        supported ? (
          <>
            <Typography.Text type="secondary">
              {t('pages.inbounds.form.usageMultiplierFutureOnly')}
            </Typography.Text>
            {savedOn === false && (
              <>
                <br />
                <Typography.Text type="secondary">
                  {t('pages.inbounds.form.usageMultiplierLocked')}
                </Typography.Text>
              </>
            )}
          </>
        ) : (
          <Typography.Text type="secondary">
            {t('pages.inbounds.form.usageMultiplierUnsupported')}
          </Typography.Text>
        )
      }
    >
      <InputNumber min={0} max={10} step={0.1} disabled={!editable} />
    </FormField>
  );
}
