import { useState } from 'react';
import { CheckCircle2, Circle, Download, Power, Radio, Square } from 'lucide-react';
import { ChannelCard } from '../components/ChannelCard';
import { StreamSettings } from '../components/StreamSettings';
import { VideoStage } from '../components/VideoStage';
import { StatsBar } from '../components/StatsBar';
import { usePublisher } from '../hooks/usePublisher';
import { usePeerStats } from '../hooks/usePeerStats';
import { useChannelStatus } from '../hooks/useChannelStatus';
import { useApplication } from '../hooks/useApplication';
import { loadIdentity, saveIdentity } from '../lib/identity';
import type { Identity, ServerConfig, StreamSettings as Settings } from '../types';

export function Studio({
  config,
  onShutdown,
}: {
  config: ServerConfig | null;
  onShutdown: () => void;
}) {
  const application = useApplication();
  const [identity, setIdentity] = useState(loadIdentity);
  const [saveError, setSaveError] = useState('');
  const [settings, setSettings] = useState<Settings>({
    fps: 60,
    bitrate: 20,
    videoPriority: 'motion',
    audio: 'window',
  });
  const publisher = usePublisher(identity, config);
  const stats = usePeerStats(publisher.pc, true);
  const { status, reachable } = useChannelStatus(identity?.nickname ?? '');

  const updating = application.updatePhase !== 'idle';
  const busy = publisher.phase !== 'idle' || application.stopping;
  const live = publisher.phase === 'live';

  async function shutdown() {
    if (!(await application.shutdown())) return;

    publisher.stop();
    onShutdown();
  }

  function remember(value: Identity) {
    setIdentity(value);

    try {
      saveIdentity(value);
      setSaveError('');
    } catch {
      setSaveError(
        'Браузер не сохранил ключ. Скачайте его в настройках канала перед закрытием страницы.',
      );
    }
  }

  return (
    <>
      <header className="topbar studio-topbar">
        <h1>Студия</h1>
        <div className="studio-status">
          <span className="server-indicator">
            <span className={`tiny-dot ${config && reachable ? 'green' : ''}`} />
            {config && reachable ? 'Сервер доступен' : 'Соединяемся с сервером'}
          </span>
          <span className={`status-pill ${live ? 'on' : ''}`}>
            <span className="status-dot" />
            {live ? 'В эфире' : publisher.phase === 'starting' ? 'Подключение' : 'Не в эфире'}
          </span>
          {application.available && (
            <button
              className="button secondary shutdown-button"
              disabled={application.stopping || updating}
              onClick={() => void shutdown()}
              title="Завершить эфир и полностью закрыть Sidecast"
            >
              <Power size={15} />
              {application.stopping ? 'Выключаем…' : 'Выключить приложение'}
            </button>
          )}
        </div>
      </header>
      <main className="studio-content host-studio">
        {application.available && application.updateInfo?.available && (
          <section className="card update-notice" aria-label="Обновление Sidecast">
            <span className="update-notice-icon">
              <Download size={19} />
            </span>
            <div className="update-notice-content">
              <h2>Доступна новая версия Sidecast {application.updateInfo.latestVersion}</h2>
              <p>
                Установлена {application.updateInfo.currentVersion}.{' '}
                {application.updateInfo.canUpdate
                  ? 'Хотите обновить приложение?'
                  : 'Скачайте новый файл из релиза GitHub.'}
              </p>
              {updating && (
                <p role="status">
                  {application.updatePhase === 'downloading'
                    ? 'Загружаем и проверяем обновление… Эфир продолжится до перезапуска.'
                    : 'Перезапускаем приложение… Эта вкладка обновится автоматически.'}
                </p>
              )}
              {!updating &&
                (application.updateError || application.updateInfo.status === 'failed') && (
                  <p className="update-notice-error" role="alert">
                    {application.updateError ||
                      application.updateInfo.error ||
                      'Не удалось установить обновление.'}{' '}
                    Можно скачать релиз вручную.
                  </p>
                )}
            </div>
            <div className="update-notice-actions">
              {application.updateInfo.canUpdate && (
                <button
                  className="button primary"
                  disabled={updating || application.stopping}
                  onClick={() => void application.update()}
                >
                  <Download size={15} />
                  {updating ? 'Обновляем…' : 'Обновить'}
                </button>
              )}
              {application.updateInfo.releaseUrl && (
                <a
                  className="update-release-link"
                  href={application.updateInfo.releaseUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Открыть релиз
                </a>
              )}
            </div>
          </section>
        )}
        {application.error && (
          <p className="error shutdown-error" role="alert">
            Не удалось выключить приложение. {application.error}
          </p>
        )}
        <div className="studio-grid">
          <div className="main-column">
            {!identity && (
              <ChannelCard
                identity={null}
                config={config}
                onIdentity={remember}
                disabled={!config || busy || updating}
              />
            )}
            <section className="card preview-card">
              <div className="preview-heading">
                <div className="section-heading">
                  <span className="section-icon">
                    <Radio size={18} />
                  </span>
                  <h2>Трансляция</h2>
                </div>
                {identity && <span className="subtle">@{identity.nickname}</span>}
              </div>
              <VideoStage stream={publisher.stream} preview live={live} />
              <StatsBar stats={stats} viewers={status?.viewers ?? 0} live={live} />
              <div className="broadcast-actions">
                <div>
                  <span className={`action-status ${live ? 'text-green' : ''}`}>
                    {live ? <CheckCircle2 size={17} /> : <Circle size={16} />}
                    {live
                      ? 'Вы в эфире'
                      : busy
                        ? 'Подключаем источник…'
                        : identity
                          ? 'Готово к запуску'
                          : 'Создайте канал для запуска'}
                  </span>
                  {live && (
                    <p>
                      {publisher.stream?.getAudioTracks().length
                        ? 'Видео и звук передаются'
                        : 'Видео передаётся · аудиодорожка отсутствует'}
                    </p>
                  )}
                </div>
                <button
                  className={`button ${busy ? 'stop' : 'primary'}`}
                  disabled={
                    !identity ||
                    !config ||
                    application.stopping ||
                    (updating && publisher.phase === 'idle')
                  }
                  onClick={() => (busy ? publisher.stop() : void publisher.start(settings))}
                >
                  {busy ? <Square size={15} fill="currentColor" /> : <Radio size={17} />}
                  {busy ? 'Завершить эфир' : 'Начать трансляцию'}
                </button>
              </div>
              {publisher.error && (
                <p className="error inline-error" role="alert">
                  {publisher.error}
                </p>
              )}
            </section>
            {identity && (
              <ChannelCard
                identity={identity}
                config={config}
                onIdentity={remember}
                disabled={!config || busy || updating}
              />
            )}
            {saveError && (
              <p className="error" role="alert">
                {saveError}
              </p>
            )}
          </div>
          <div className="side-column">
            <StreamSettings value={settings} onChange={setSettings} disabled={busy || updating} />
          </div>
        </div>
      </main>
    </>
  );
}
