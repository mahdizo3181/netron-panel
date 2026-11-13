import { Outlet } from 'react-router';

import { useWebSocketBridge } from '@/api/websocketBridge';
import { usePageTitle } from '@/hooks/usePageTitle';
import CommandPalette from '@/components/command-palette/CommandPalette';
import { isResellerUi } from '@/lib/role';

function AdminLayout() {
  useWebSocketBridge();
  usePageTitle();
  return (
    <>
      <Outlet />
      <CommandPalette />
    </>
  );
}

// No WebSocket and no command palette: both carry admin data; reseller pages poll.
function ResellerLayout() {
  usePageTitle();
  return <Outlet />;
}

export default function PanelLayout() {
  return isResellerUi() ? <ResellerLayout /> : <AdminLayout />;
}
