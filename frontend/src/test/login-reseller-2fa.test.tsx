import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import LoginPage from '@/pages/login/LoginPage';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';

afterEach(() => vi.restoreAllMocks());

// The global 2FA switch is the admin's; a reseller with its own 2FA learns it
// needs a code only after its password matched, and must then be able to type it.
describe('LoginPage two-factor prompt', () => {
  it('shows the code field when the server asks for a two-factor code', async () => {
    const post = vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string) => {
      if (url === '/getTwoFactorEnable') return new Msg(true, '', false);
      return new Msg(false, 'Enter your two-factor code.', { twoFactorRequired: true });
    });
    renderWithProviders(<LoginPage />);

    const user = await screen.findByPlaceholderText('Username');
    expect(screen.queryByPlaceholderText('Code')).toBeNull();
    fireEvent.change(user, { target: { value: 'shop-a' } });
    fireEvent.change(screen.getByPlaceholderText('Password'), {
      target: { value: 'correct-horse-battery' },
    });
    fireEvent.click(screen.getByRole('button', { name: /log ?in/i }));

    await waitFor(() => expect(post).toHaveBeenCalledWith('/login', expect.anything()));
    expect(await screen.findByPlaceholderText('Code')).toBeTruthy();
  });

  it('keeps the field hidden after an ordinary refusal', async () => {
    const post = vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string) => {
      if (url === '/getTwoFactorEnable') return new Msg(true, '', false);
      return new Msg(false, 'Invalid username or password or two-factor code.');
    });
    renderWithProviders(<LoginPage />);

    fireEvent.change(await screen.findByPlaceholderText('Username'), { target: { value: 'x' } });
    fireEvent.change(screen.getByPlaceholderText('Password'), { target: { value: 'y' } });
    fireEvent.click(screen.getByRole('button', { name: /log ?in/i }));

    await waitFor(() => expect(post).toHaveBeenCalledWith('/login', expect.anything()));
    expect(screen.queryByPlaceholderText('Code')).toBeNull();
  });
});
