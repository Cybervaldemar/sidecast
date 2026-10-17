import { useCallback, useEffect, useRef, useState } from 'react';
import { api, errorMessage } from '../lib/api';
import type { Identity, NativeOptions, NativeStatus, StreamSettings } from '../types';

export function useNativePublisher(identity: Identity | null, enabled: boolean, options: NativeOptions) {
  const [status, setStatus] = useState<NativeStatus>({ phase: 'idle', stats: {} });
  const [error, setError] = useState('');
  const pending = useRef<AbortController | null>(null); const generation = useRef(0);
  const stop = useCallback(() => {
    generation.current++; pending.current?.abort(); pending.current = null;
    if (identity) void api('/native/stop', { method: 'DELETE', keepalive: true }, identity.token).catch(() => {});
    setStatus({ phase: 'idle', stats: {} });
  }, [identity]);
  useEffect(() => {
    if (!enabled || !identity) return;
    let active = true; let timer: ReturnType<typeof setTimeout>; const abort = new AbortController();
    async function poll() {
      try { const next = await api<NativeStatus>('/native/status', { signal: abort.signal }, identity!.token); if (active && !pending.current) { setStatus(next); if (next.error) setError(next.error); } }
      catch (e) { if (active) setError(errorMessage(e)); }
      if (active) timer = setTimeout(poll, 1000);
    }
    void poll();
    const pagehide = () => stop(); window.addEventListener('pagehide', pagehide);
    return () => { active = false; clearTimeout(timer); abort.abort(); window.removeEventListener('pagehide', pagehide); stop(); };
  }, [identity, enabled, stop]);
  const start = async (settings: StreamSettings) => {
    if (!enabled || !identity || pending.current || status.phase !== 'idle') return;
    const revision = ++generation.current; const abort = new AbortController(); pending.current = abort;
    const timeout = setTimeout(() => abort.abort(), 60000);
    setError(''); setStatus({ phase: 'starting', stats: {} });
    try {
      const next = await api<NativeStatus>('/native/start', { method: 'POST', signal: abort.signal, body: JSON.stringify({ nickname: identity.nickname, settings: { ...options, fps: settings.fps, bitrate: settings.bitrate, audio: settings.audio } }) }, identity.token);
      if (revision === generation.current) setStatus(next);
    } catch (e) { if (revision === generation.current) { setStatus({ phase: 'idle', stats: {} }); setError(errorMessage(e)); } }
    finally { clearTimeout(timeout); if (pending.current === abort) pending.current = null; }
  };
  return { start, stop, phase: status.phase, error, stats: status.stats, encoder: status.encoder, stream: null, pc: null };
}
