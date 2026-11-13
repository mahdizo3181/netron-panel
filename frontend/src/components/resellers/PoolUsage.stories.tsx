import type { Meta, StoryObj } from '@storybook/react-vite';

import PoolUsage from './PoolUsage';

const meta = {
  title: 'Resellers/PoolUsage',
  component: PoolUsage,
  tags: ['autodocs'],
  parameters: {
    docs: {
      description: {
        component:
          "A reseller's volume pool: progress bar with percentage, then total, used and remaining. The numbers are the server's pool counter, never a sum of client usage. A state banner explains an exhausted, expired or disabled account.",
      },
    },
  },
  argTypes: {
    poolBytes: { description: 'Total pool in bytes; 0 means no pool limit.' },
    usedTraffic: { description: 'Billed bytes counted against the pool.' },
    state: { description: '`ok`, `pool`, `expired` or `disabled`, as `/reseller/me` reports it.' },
    compact: { description: 'One column and no state banner, for table rows.' },
  },
} satisfies Meta<typeof PoolUsage>;

export default meta;
type Story = StoryObj<typeof meta>;

export const HalfUsed: Story = {
  args: { poolBytes: 1000 * 2 ** 30, usedTraffic: 512 * 2 ** 30, state: 'ok' },
};
export const Exhausted: Story = {
  args: { poolBytes: 100 * 2 ** 30, usedTraffic: 101 * 2 ** 30, state: 'pool' },
};
export const Expired: Story = {
  args: { poolBytes: 100 * 2 ** 30, usedTraffic: 10 * 2 ** 30, state: 'expired' },
};
export const Unlimited: Story = { args: { poolBytes: 0, usedTraffic: 42 * 2 ** 30, state: 'ok' } };
export const Compact: Story = {
  args: { poolBytes: 500 * 2 ** 30, usedTraffic: 450 * 2 ** 30, compact: true },
};
