import type { Meta, StoryObj } from '@storybook/react-vite';

import OwnerTag from './OwnerTag';

const meta = {
  title: 'Resellers/OwnerTag',
  component: OwnerTag,
  tags: ['autodocs'],
  parameters: {
    docs: {
      description: {
        component:
          "Owner badge in the admin's client list: the reseller's name for its clients, a neutral \"admin\" tag for the admin's own. Resellers never see it.",
      },
    },
  },
  argTypes: {
    owner: {
      description: 'The read-only `owner` of a client row: `{ id, name }`, or null for the admin.',
    },
  },
} satisfies Meta<typeof OwnerTag>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Reseller: Story = { args: { owner: { id: 3, name: 'Shop A' } } };
export const PersianName: Story = { args: { owner: { id: 4, name: 'فروشگاه تهران' } } };
export const Admin: Story = { args: { owner: null } };
