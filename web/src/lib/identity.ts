import type { Identity } from '../types';

const key = 'sidecast.identity.v2';
const legacyKey = 'lanplay.identity.v2';

export function loadIdentity(): Identity | null {
  // Keep channels created before the rename accessible in the same browser.
  for (const storageKey of [key, legacyKey]) {
    try {
      const saved = JSON.parse(localStorage.getItem(storageKey) ?? 'null');

      if (saved && typeof saved.nickname === 'string' && /^[a-f0-9]{64}$/.test(saved.token))
        return saved;
    } catch {
      // An unreadable entry must not prevent recovery from the previous key.
    }
  }

  return null;
}

export function saveIdentity(identity: Identity) {
  localStorage.setItem(key, JSON.stringify(identity));
}
