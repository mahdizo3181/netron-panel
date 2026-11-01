import type { ReactElement } from 'react';
import { describe, expect, it } from 'vitest';
import { renderHook, render } from '@testing-library/react';

import { useInboundColumns } from '@/pages/inbounds/list/useInboundColumns';
import type { DBInboundRecord } from '@/pages/inbounds/list/types';
import { DBInbound } from '@/models/dbinbound';

function protocolCell(usageMultiplier: number | null | undefined): string {
  const { result } = renderHook(() =>
    useInboundColumns({
      hasAnyRemark: false,
      hasAnySubSortIndex: false,
      hasActiveNode: false,
      nodesById: new Map(),
      hostRemarksByInboundId: new Map(),
      clientCount: {},
      inboundSpeed: {},
      subEnable: false,
      expireDiff: 0,
      trafficDiff: 0,
      onRowAction: () => {},
      onSwitchEnable: () => {},
    }),
  );
  const column = result.current.find((c) => c.key === 'protocol');
  const record = new DBInbound({
    protocol: 'vless',
    usageMultiplier,
  }) as unknown as DBInboundRecord;
  const cell = column?.render?.(undefined, record, 0) as ReactElement;
  const { container } = render(cell);
  return container.textContent ?? '';
}

describe('inbounds table multiplier badge', () => {
  it('shows x1.2 on a weighted inbound', () => {
    expect(protocolCell(1.2)).toContain('x1.2');
  });

  it('shows x0 for a free inbound', () => {
    expect(protocolCell(0)).toContain('x0');
  });

  it('shows nothing for x1 or an unset multiplier', () => {
    expect(protocolCell(1)).not.toMatch(/x\d/);
    expect(protocolCell(undefined)).not.toMatch(/x\d/);
    expect(protocolCell(null)).not.toMatch(/x\d/);
  });
});
