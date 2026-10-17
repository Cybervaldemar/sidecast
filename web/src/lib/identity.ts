import type { Identity } from '../types';
const key = 'lanplay.identity.v2';
export function loadIdentity(): Identity | null {
  try { const saved = JSON.parse(localStorage.getItem(key) ?? 'null'); return saved && typeof saved.nickname === 'string' && /^[a-f0-9]{64}$/.test(saved.token) ? saved : null; }
  catch { return null; }
}
export function saveIdentity(identity: Identity) { localStorage.setItem(key, JSON.stringify(identity)); }
