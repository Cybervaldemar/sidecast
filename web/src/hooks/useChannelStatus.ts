import { useEffect, useState } from 'react';
import { getStatus } from '../lib/api';
import type { ChannelStatus } from '../types';
export function useChannelStatus(nickname: string) {
  const [status, setStatus] = useState<ChannelStatus | null>(null); const [reachable, setReachable] = useState(true);
  useEffect(() => {
    setStatus(null); if (!nickname) return;
    const controller = new AbortController(); let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try { const next = await getStatus(nickname, controller.signal); if (!controller.signal.aborted) { setStatus(next); setReachable(true); } }
      catch { if (!controller.signal.aborted) setReachable(false); }
      finally { if (!controller.signal.aborted) timer = setTimeout(poll, 2000); }
    }
    void poll(); return () => { controller.abort(); clearTimeout(timer); };
  }, [nickname]); return { status, reachable };
}
