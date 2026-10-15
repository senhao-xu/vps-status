// Only the homepage sign-in entry may leave the administrator route after auth.
export function returnHomeAfterAuth(pathname: string, search: string): boolean {
  return pathname === '/admin' && search === '?next=home'
}
