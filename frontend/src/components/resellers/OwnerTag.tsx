import { useTranslation } from 'react-i18next';
import { Tag } from 'antd';

import type { ClientOwner } from '@/generated/types';

// The admin's own clients get a neutral tag; a reseller's carry its name.
export default function OwnerTag({ owner }: { owner?: ClientOwner | null }) {
  const { t } = useTranslation();
  if (!owner) {
    return (
      <Tag className="owner-tag" data-owner="admin">
        {t('pages.resellers.ownerAdmin')}
      </Tag>
    );
  }
  return (
    <Tag className="owner-tag" color="purple" data-owner={owner.id} title={owner.name}>
      {owner.name}
    </Tag>
  );
}
