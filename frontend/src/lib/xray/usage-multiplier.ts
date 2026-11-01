import { Protocols } from '@/schemas/primitives';

/*
 * Protocols whose clients reach Xray as regular users, so the backend can bill
 * them per inbound. Every other protocol is held at x1 (the API rejects more).
 * Mirrors usagemult.SupportsProtocol in internal/usagemult.
 */
const MULTIPLIER_PROTOCOLS: ReadonlySet<string> = new Set([
  Protocols.VLESS,
  Protocols.VMESS,
  Protocols.TROJAN,
  Protocols.SHADOWSOCKS,
  Protocols.HYSTERIA,
  Protocols.MASQUE,
]);

export const USAGE_MULTIPLIER_CHIPS = [1, 1.2, 1.5, 2] as const;

export function supportsUsageMultiplier(protocol: string): boolean {
  return MULTIPLIER_PROTOCOLS.has(protocol);
}

// An unset multiplier means x1, never free.
export function effectiveUsageMultiplier(value: number | null | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 1;
}

export function formatUsageMultiplier(value: number): string {
  return `x${Number(value.toFixed(2))}`;
}
