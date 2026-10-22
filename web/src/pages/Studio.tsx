import { useState } from 'react';
import { CheckCircle2, Circle, Power, Radio, Square } from 'lucide-react';
import { ChannelCard } from '../components/ChannelCard';
import { StreamSettings } from '../components/StreamSettings';
import { VideoStage } from '../components/VideoStage';
import { StatsBar } from '../components/StatsBar';
import { CaptureMode } from '../components/CaptureMode';
import { usePublisher } from '../hooks/usePublisher';
import { useNativePublisher } from '../hooks/useNativePublisher';
import { usePeerStats } from '../hooks/usePeerStats';
import { useChannelStatus } from '../hooks/useChannelStatus';
import { useApplication } from '../hooks/useApplication';
import { loadIdentity, saveIdentity } from '../lib/identity';
import type { Identity, NativeOptions, ServerConfig, StreamSettings as Settings } from '../types';

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
  const [settings, setSettings] = useState<Settings>({ fps: 60, bitrate: 12, audio: 'window' });
  const [mode, setMode] = useState<'browser' | 'native'>('browser');
  const [nativeOptions, setNativeOptions] = useState<NativeOptions>({
    sourceId: '',
    audioSourceId: '',
    encoder: 'auto',
  });

  const browserPublisher = usePublisher(identity, config);
  const nativePublisher = useNativePublisher(
    identity,
    mode === 'native' && !!config?.nativeCapture?.available,
    nativeOptions,
  );
  const publisher = mode === 'native' ? nativePublisher : browserPublisher;

  const browserStats = usePeerStats(browserPublisher.pc, true);
  const stats = mode === 'native' ? nativePublisher.stats : browserStats;
  const { status, reachable } = useChannelStatus(identity?.nickname ?? '');

  const busy = publisher.phase !== 'idle' || application.stopping;
  const live = publisher.phase === 'live';

  async function shutdown() {
    if (!(await application.shutdown())) return;

    browserPublisher.stop();
    nativePublisher.stop();
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
              disabled={application.stopping}
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
                disabled={!config || busy}
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
              <VideoStage
                stream={publisher.stream}
                preview
                live={live}
                title={
                  mode === 'native'
                    ? live
                      ? 'Нативная трансляция запущена'
                      : 'Нативный захват Windows'
                    : undefined
                }
                subtitle={
                  mode === 'native'
                    ? live
                      ? 'Видео передаётся на сервер.\nПроверить изображение можно по ссылке зрителя.'
                      : 'Выберите окно или экран и источник звука.\nИзображение захватывается средствами Windows.'
                    : undefined
                }
              />
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
                      {mode === 'native'
                        ? `${settings.audio === 'off' ? 'Нативное видео · без звука' : 'Нативное видео и звук'} · ${nativePublisher.encoder ?? 'H.264'}`
                        : `${publisher.stream?.getAudioTracks().length ? 'Видео и звук передаются' : 'Видео передаётся · аудиодорожка отсутствует'}`}
                    </p>
                  )}
                </div>
                <button
                  className={`button ${busy ? 'stop' : 'primary'}`}
                  disabled={
                    !identity ||
                    !config ||
                    (!busy &&
                      mode === 'native' &&
                      (!nativeOptions.sourceId ||
                        (settings.audio === 'window' && !nativeOptions.audioSourceId)))
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
                disabled={!config || busy}
              />
            )}
            {saveError && (
              <p className="error" role="alert">
                {saveError}
              </p>
            )}
          </div>
          <div className="side-column">
            <CaptureMode
              capabilities={config?.nativeCapture}
              mode={mode}
              onMode={setMode}
              options={nativeOptions}
              onOptions={setNativeOptions}
              audio={settings.audio}
              disabled={busy}
            />
            <StreamSettings
              value={settings}
              onChange={setSettings}
              disabled={busy}
              native={mode === 'native' ? config?.nativeCapture : undefined}
            />
          </div>
        </div>
      </main>
    </>
  );
}
