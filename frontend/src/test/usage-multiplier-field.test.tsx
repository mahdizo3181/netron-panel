import { useEffect, type ReactNode } from 'react';
import { describe, expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { Form } from 'antd';
import { FormProvider, useForm } from 'react-hook-form';

import UsageMultiplierField from '@/pages/inbounds/form/UsageMultiplierField';
import { renderWithProviders } from './test-utils';

const form: { read: () => unknown } = { read: () => undefined };

function Harness({ children, value = 1 }: { children: ReactNode; value?: number }) {
  const methods = useForm({ defaultValues: { usageMultiplier: value } });
  useEffect(() => {
    form.read = () => methods.getValues('usageMultiplier');
  }, [methods]);
  return (
    <FormProvider {...methods}>
      <Form>{children}</Form>
    </FormProvider>
  );
}

describe('UsageMultiplierField', () => {
  it('quick chips set the multiplier and the future-only note is shown', () => {
    renderWithProviders(
      <Harness>
        <UsageMultiplierField protocol="vless" />
      </Harness>,
    );
    expect(screen.getByText('Applies to future traffic only')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'x1.5' }));
    expect(form.read()).toBe(1.5);
    fireEvent.click(screen.getByRole('button', { name: 'x1' }));
    expect(form.read()).toBe(1);
  });

  it('is disabled with a note where the backend only accepts x1', () => {
    renderWithProviders(
      <Harness>
        <UsageMultiplierField protocol="wireguard" />
      </Harness>,
    );
    expect(document.querySelector('.ant-input-number-disabled')).not.toBeNull();
    expect(screen.queryByRole('button', { name: 'x1.2' })).toBeNull();
    expect(screen.getByText(/Not available for this protocol/)).toBeTruthy();
  });
});
