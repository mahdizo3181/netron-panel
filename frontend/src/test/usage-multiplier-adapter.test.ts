import { describe, expect, it } from 'vitest';

import { formValuesToWirePayload, rawInboundToFormValues } from '@/lib/xray/inbound-form-adapter';
import {
  effectiveUsageMultiplier,
  formatUsageMultiplier,
  supportsUsageMultiplier,
} from '@/lib/xray/usage-multiplier';

const row = { protocol: 'vless', port: 1234, settings: { clients: [] }, tag: 'in-1' };

describe('usage multiplier wire format', () => {
  it('sends the entered multiplier with the other inbound fields', () => {
    const values = rawInboundToFormValues({ ...row, usageMultiplier: 1.2 });
    expect(formValuesToWirePayload(values).usageMultiplier).toBe(1.2);
  });

  // An inbound saved before the feature (or an old node's row) has no field: x1, never free.
  it('reads a row without the field as x1', () => {
    expect(rawInboundToFormValues(row).usageMultiplier).toBe(1);
    expect(rawInboundToFormValues({ ...row, usageMultiplier: null }).usageMultiplier).toBe(1);
  });

  it('keeps an explicit 0 (free) instead of coercing it to x1', () => {
    const values = rawInboundToFormValues({ ...row, usageMultiplier: 0 });
    expect(values.usageMultiplier).toBe(0);
    expect(formValuesToWirePayload(values).usageMultiplier).toBe(0);
  });

  // Switching a x1.2 inbound to WireGuard would otherwise be refused by the API.
  it('holds unsupported protocols at x1 on the wire', () => {
    const values = {
      ...rawInboundToFormValues({ ...row, usageMultiplier: 1.2 }),
      protocol: 'tuic',
    };
    expect(formValuesToWirePayload(values as never).usageMultiplier).toBe(1);
  });
});

describe('usage multiplier helpers', () => {
  it('supports exactly the protocols the backend bills per client', () => {
    for (const p of ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria', 'masque']) {
      expect(supportsUsageMultiplier(p)).toBe(true);
    }
    for (const p of [
      'wireguard',
      'tuic',
      'mtproto',
      'amneziawg',
      'tunnel',
      'http',
      'mixed',
      'tun',
    ]) {
      expect(supportsUsageMultiplier(p)).toBe(false);
    }
  });

  it('formats badges without float noise', () => {
    expect(formatUsageMultiplier(1.2)).toBe('x1.2');
    expect(formatUsageMultiplier(2)).toBe('x2');
    expect(formatUsageMultiplier(0.1 + 0.2)).toBe('x0.3');
    expect(effectiveUsageMultiplier(undefined)).toBe(1);
    expect(effectiveUsageMultiplier(0)).toBe(0);
  });
});
