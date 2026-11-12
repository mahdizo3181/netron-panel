import { useEffect, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { Form } from 'antd';
import { FormProvider, useForm } from 'react-hook-form';

import UsageMultiplierField from '@/pages/inbounds/form/UsageMultiplierField';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';

const form: { read: () => unknown } = { read: () => undefined };
const settingsHint = 'Enable the per-inbound multiplier in Settings first';

afterEach(() => vi.restoreAllMocks());

function mockSavedSwitch(configured: boolean | 'loading') {
  vi.spyOn(HttpUtil, 'post').mockImplementation(async () => {
    if (configured === 'loading') return new Promise<never>(() => {});
    return new Msg(true, '', { configured, active: false, nodes: [] });
  });
}

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

function renderField(protocol: string, value?: number) {
  renderWithProviders(
    <Harness value={value}>
      <UsageMultiplierField protocol={protocol} />
    </Harness>,
  );
}

const inputDisabled = () => document.querySelector('.ant-input-number-disabled') !== null;

describe('UsageMultiplierField', () => {
  it('quick chips set the multiplier while the saved switch is ON', async () => {
    mockSavedSwitch(true);
    renderField('vless');
    const chip = await screen.findByRole('button', { name: 'x1.5' });
    await vi.waitFor(() => expect((chip as HTMLButtonElement).disabled).toBe(false));
    expect(inputDisabled()).toBe(false);
    expect(screen.getByText('Applies to future traffic only')).toBeTruthy();
    expect(screen.queryByText(settingsHint)).toBeNull();
    fireEvent.click(chip);
    expect(form.read()).toBe(1.5);
  });

  it('is greyed out with the Settings hint and ignores chips while the saved switch is OFF', async () => {
    mockSavedSwitch(false);
    renderField('vless', 4);
    expect(await screen.findByText(settingsHint)).toBeTruthy();
    expect(inputDisabled()).toBe(true);
    const chip = screen.getByRole('button', { name: 'x1.5' }) as HTMLButtonElement;
    expect(chip.disabled).toBe(true);
    fireEvent.click(chip);
    expect(form.read()).toBe(4);
  });

  it('stays disabled, without the Settings hint, until the saved switch has loaded', () => {
    mockSavedSwitch('loading');
    renderField('vless');
    expect(inputDisabled()).toBe(true);
    expect((screen.getByRole('button', { name: 'x1.2' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByText(settingsHint)).toBeNull();
  });

  it('keeps the protocol note, not the Settings hint, where the backend only accepts x1', async () => {
    mockSavedSwitch(false);
    renderField('wireguard');
    expect(screen.getByText(/Not available for this protocol/)).toBeTruthy();
    expect(inputDisabled()).toBe(true);
    expect(screen.queryByRole('button', { name: 'x1.2' })).toBeNull();
    await vi.waitFor(() => expect(HttpUtil.post).toHaveBeenCalled());
    expect(screen.queryByText(settingsHint)).toBeNull();
  });
});
