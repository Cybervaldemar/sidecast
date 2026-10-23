import { useEffect, useState } from 'react';
import { Power } from 'lucide-react';
import { getConfig } from './lib/api';
import { Sidebar } from './components/Sidebar';
import { Help } from './components/Help';
import { Studio } from './pages/Studio';
import { Watch } from './pages/Watch';
import type { ServerConfig } from './types';

export function App() {
  const [config, setConfig] = useState<ServerConfig | null>(null);
  const [error, setError] = useState(false);
  const [page, setPage] = useState<'studio' | 'help'>('studio');
  const [stopped, setStopped] = useState(false);

  useEffect(() => {
    if (stopped) return;

    let active = true;
    let timer: ReturnType<typeof setTimeout>;

    async function load() {
      try {
        const next = await getConfig();

        if (active) {
          setConfig((current) =>
            JSON.stringify(current) === JSON.stringify(next) ? current : next,
          );
          setError(false);
          timer = setTimeout(load, 15000);
        }
      } catch {
        if (active) {
          setError(true);
          timer = setTimeout(load, 3000);
        }
      }
    }

    void load();

    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [stopped]);

  const match = location.pathname.match(/^\/watch\/([a-z0-9][a-z0-9_-]{2,31})$/);

  if (match) return <Watch nickname={match[1]} config={config} />;

  if (stopped)
    return (
      <main className="application-stopped">
        <Power size={32} />
        <h1>Приложение выключено</h1>
        <p>Для новой трансляции запустите Sidecast снова. Эту вкладку можно закрыть.</p>
      </main>
    );

  return (
    <div className="app-shell">
      <Sidebar page={page} onPage={setPage} />
      <div className="app-body">
        {error && (
          <div className="connection-banner" role="alert">
            Сервер недоступен. Повторяем подключение…
          </div>
        )}
        <div hidden={page !== 'studio'}>
          <Studio config={config} onShutdown={() => setStopped(true)} />
        </div>
        {page === 'help' && (
          <main className="studio-content">
            <Help />
          </main>
        )}
      </div>
    </div>
  );
}
