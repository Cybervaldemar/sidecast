import { useEffect, useRef, useState } from 'react';
import { api, errorMessage } from '../lib/api';

export function useApplication() {
  const [token, setToken] = useState('');
  const [stopping, setStopping] = useState(false);
  const [error, setError] = useState('');
  const locked = useRef(false);

  useEffect(() => {
    const controller = new AbortController();

    void api<{ shutdownToken: string }>('/app', { signal: controller.signal })
      .then((value) => setToken(value.shutdownToken))
      .catch(() => {});

    return () => controller.abort();
  }, []);

  async function shutdown() {
    if (!token || locked.current) return false;

    locked.current = true;
    setStopping(true);
    setError('');

    try {
      await api('/app/shutdown', { method: 'POST' }, token);

      return true;
    } catch (cause) {
      locked.current = false;
      setStopping(false);
      setError(errorMessage(cause));

      return false;
    }
  }

  return { available: !!token, stopping, error, shutdown };
}
