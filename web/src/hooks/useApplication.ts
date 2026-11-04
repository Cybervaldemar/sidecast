import { useEffect, useRef, useState } from 'react';
import { api, errorMessage } from '../lib/api';

interface UpdateInfo {
  currentVersion: string;
  latestVersion: string;
  available: boolean;
  canUpdate: boolean;
  releaseUrl: string;
  status: 'idle' | 'downloading' | 'restarting' | 'failed';
  error?: string;
}

type UpdatePhase = 'idle' | 'downloading' | 'restarting';

const pause = (milliseconds: number) =>
  new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds));

export function useApplication() {
  const [token, setToken] = useState('');
  const [stopping, setStopping] = useState(false);
  const [error, setError] = useState('');
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [updatePhase, setUpdatePhase] = useState<UpdatePhase>('idle');
  const [updateError, setUpdateError] = useState('');
  const locked = useRef(false);

  useEffect(() => {
    const controller = new AbortController();

    void api<{ shutdownToken: string }>('/app', { signal: controller.signal })
      .then((value) => setToken(value.shutdownToken))
      .catch(() => {});

    return () => controller.abort();
  }, []);

  useEffect(() => {
    if (!token) return;

    const controller = new AbortController();
    const check = async () => {
      if (locked.current) return;

      try {
        const info = await api<UpdateInfo>('/app/update', { signal: controller.signal }, token);
        setUpdateInfo(info);
      } catch {
        // A disconnected GitHub check should not interrupt the studio.
      }
    };

    void check();
    const timer = window.setInterval(() => void check(), 45 * 60 * 1000);

    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [token]);

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

  async function update() {
    if (!token || !updateInfo?.available || !updateInfo.canUpdate || locked.current) return;

    locked.current = true;
    setUpdateError('');
    setUpdatePhase('downloading');

    try {
      await api('/app/update', { method: 'POST' }, token);

      const expectedVersion = updateInfo.latestVersion.replace(/^v/, '');
      const previousVersion = updateInfo.currentVersion.replace(/^v/, '');
      const deadline = Date.now() + 5 * 60 * 1000;
      let sawOffline = false;
      let oldVersionChecks = 0;

      while (Date.now() < deadline) {
        const [health, progress] = await Promise.allSettled([
          api<{ version: string }>('/health', { signal: AbortSignal.timeout(3000) }),
          api<UpdateInfo>('/app/update', { signal: AbortSignal.timeout(3000) }, token),
        ]);

        if (
          health.status === 'fulfilled' &&
          health.value.version.replace(/^v/, '') === expectedVersion
        ) {
          window.location.reload();
          return;
        }

        if (health.status === 'rejected') {
          sawOffline = true;
          oldVersionChecks = 0;
        } else if (sawOffline && health.value.version.replace(/^v/, '') === previousVersion) {
          oldVersionChecks++;

          if (oldVersionChecks >= 3) {
            const instance = await api<{ shutdownToken: string }>('/app', {
              signal: AbortSignal.timeout(3000),
            }).catch(() => null);

            if (instance?.shutdownToken && instance.shutdownToken !== token) {
              throw new Error('Не удалось установить обновление: запустилась прежняя версия.');
            }

            if (instance?.shutdownToken === token) {
              sawOffline = false;
              oldVersionChecks = 0;
            }
          }
        } else {
          oldVersionChecks = 0;
        }

        if (progress.status === 'fulfilled') {
          setUpdateInfo(progress.value);

          if (progress.value.status === 'failed') {
            throw new Error(progress.value.error || 'Не удалось установить обновление.');
          }

          if (progress.value.status === 'restarting') setUpdatePhase('restarting');
        } else if (health.status === 'rejected') {
          setUpdatePhase('restarting');
        }

        await pause(1500);
      }

      throw new Error('Приложение не запустилось после обновления.');
    } catch (cause) {
      setUpdateError(errorMessage(cause));
      setUpdatePhase('idle');
      locked.current = false;
    }
  }

  return {
    available: !!token,
    stopping,
    error,
    shutdown,
    updateInfo,
    updatePhase,
    updateError,
    update,
  };
}
