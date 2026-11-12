import { useEffect, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { Form } from 'antd';
import { QueryClient } from '@tanstack/react-query';
import { FormProvider, useForm } from 'react-hook-form';

import UsageMultiplierField from '@/pages/inbounds/form/UsageMultiplierField';
import { keys } from '@/api/queryKeys';
import { HttpUtil, Msg } from '@/utils';
import { makeTestQueryClient, renderWithProviders } from './test-utils';

const form: { read: () => unknown } = { read: () => undefined };
const lockedHint = "Turn on 'Enable per-inbound multiplier' in Settings to edit this.";

afterEach(() => vi.restoreAllMocks());

function mockSavedSwitch(usageMultiplierEnable: boolean) {
  return vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string) => {
    if (url.endsWith('/setting/defaultSettings'))
      return new Msg(true, '', { usageMultiplierEnable });
    return new Msg(true, '', null);
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

function renderField(protocol: string, value?: number, queryClient = makeTestQueryClient()) {
  renderWithProviders(
    <Harness value={value}>
      <UsageMultiplierField protocol={protocol} />
    </Harness>,
    { queryClient },
  );
}

const input = () => screen.getByRole('spinbutton') as HTMLInputElement;
const chip = (name: string) => screen.getByRole('button', { name }) as HTMLButtonElement;

describe('UsageMultiplierField', () => {
  it('is editable with working chips while the saved switch is ON', async () => {
    mockSavedSwitch(true);
    renderField('vless');
    await vi.waitFor(() => expect(chip('x1.5').disabled).toBe(false));
    expect(input().disabled).toBe(false);
    expect(screen.getByText('Applies to future traffic only')).toBeTruthy();
    expect(screen.queryByText(lockedHint)).toBeNull();
    fireEvent.click(chip('x1.5'));
    expect(form.read()).toBe(1.5);
  });

  it('is locked, showing the stored value and the Settings hint, while the saved switch is OFF', async () => {
    mockSavedSwitch(false);
    renderField('vless', 1.5);
    expect(await screen.findByText(lockedHint)).toBeTruthy();
    expect(input().disabled).toBe(true);
    expect(input().value).toBe('1.5');
    expect(chip('x2').disabled).toBe(true);
    fireEvent.click(chip('x2'));
    expect(form.read()).toBe(1.5);
  });

  // The setting can change while the form is closed; the form must not trust the cache.
  it('re-reads the saved switch on open instead of a cached ON', async () => {
    // Like the inbounds page, which holds this key with staleTime Infinity.
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    queryClient.setQueryData(keys.settings.defaults(), { usageMultiplierEnable: true });
    const post = mockSavedSwitch(false);
    renderField('vless', 1.5, queryClient);
    expect(await screen.findByText(lockedHint)).toBeTruthy();
    expect(input().disabled).toBe(true);
    expect(post).toHaveBeenCalledWith('/panel/api/setting/defaultSettings', undefined, {
      silent: true,
    });
  });

  it('keeps the protocol note where the backend only accepts x1', async () => {
    const post = mockSavedSwitch(true);
    renderField('wireguard');
    await vi.waitFor(() => expect(post).toHaveBeenCalled());
    expect(input().disabled).toBe(true);
    expect(screen.queryByRole('button', { name: 'x1.2' })).toBeNull();
    expect(screen.getByText(/Not available for this protocol/)).toBeTruthy();
  });
});
