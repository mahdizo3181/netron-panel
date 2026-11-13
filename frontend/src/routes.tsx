import { Suspense } from 'react';
import { Navigate, createBrowserRouter, type RouteObject } from 'react-router';
import { Spin } from 'antd';

import PanelLayout from '@/layouts/PanelLayout';
import { panelRoutesFor } from '@/routes/panelRoutes';
import { isResellerUi } from '@/lib/role';

function withSuspense(node: React.ReactNode) {
  return (
    <Suspense
      fallback={
        <div
          style={{
            display: 'flex',
            justifyContent: 'center',
            alignItems: 'center',
            minHeight: '60vh',
          }}
        >
          <Spin size="large" />
        </div>
      }
    >
      {node}
    </Suspense>
  );
}

export function buildRoutes(reseller: boolean): RouteObject[] {
  const children: RouteObject[] = panelRoutesFor(reseller).map((route) => {
    const element = withSuspense(<route.component />);
    return route.path === '/' ? { index: true, element } : { path: route.path.slice(1), element };
  });
  if (reseller) children.push({ path: '*', element: <Navigate to="/" replace /> });
  return [{ path: '/', element: <PanelLayout />, children }];
}

export const routes: RouteObject[] = buildRoutes(isResellerUi());

function computeBasename() {
  const raw = (typeof window !== 'undefined' && window.X_UI_BASE_PATH) || '/';
  const trimmed = raw.replace(/\/+$/, '');
  return `${trimmed}/panel`;
}

export const router = createBrowserRouter(routes, {
  basename: computeBasename(),
});
