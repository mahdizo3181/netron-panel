// The server marks a reseller's page; the API, not this flag, decides what it may do.
export function isResellerUi(): boolean {
  return typeof window !== 'undefined' && window.X_UI_ROLE === 'reseller';
}
