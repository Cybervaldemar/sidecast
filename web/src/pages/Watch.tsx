import { Brand } from '../components/Brand';
import { VideoStage } from '../components/VideoStage';
import { StatsBar } from '../components/StatsBar';
import { useChannelStatus } from '../hooks/useChannelStatus';
import { useViewer } from '../hooks/useViewer';
import { usePeerStats } from '../hooks/usePeerStats';
import type { ServerConfig } from '../types';

export function Watch({ nickname, config }: { nickname: string; config: ServerConfig | null }) {
  const { status, reachable } = useChannelStatus(nickname);
  const viewer = useViewer(nickname, status?.sessionId ?? '', !!status?.live && reachable, config);
  const stats = usePeerStats(viewer.pc, false);

  return (
    <div className="watch-page">
      <header className="watch-header">
        <Brand />
        <a href="/" className="button secondary">
          Открыть студию
        </a>
      </header>
      <main className="watch-content">
        <div className="watch-heading">
          <div className="avatar">{nickname[0].toUpperCase()}</div>
          <div>
            <span className="eyebrow">ПОСТОЯННЫЙ КАНАЛ</span>
            <h1>{nickname}</h1>
          </div>
          <span className={`status-pill ${status?.live ? 'on' : ''}`}>
            <span className="status-dot" />
            {status?.live ? 'В эфире' : 'Не в эфире'}
          </span>
        </div>
        <section className="card watch-player">
          <VideoStage
            stream={viewer.stream}
            live={!!status?.live && reachable}
            waiting={!status?.live}
            title={
              !reachable
                ? 'Восстанавливаем связь с сервером'
                : status?.live
                  ? 'Подключаемся к эфиру'
                  : `${nickname} сейчас не в эфире`
            }
            subtitle={
              !reachable
                ? 'Оставьте страницу открытой — попробуем снова.'
                : status?.live
                  ? 'Подготавливаем видео и звук…'
                  : 'Оставьте эту страницу открытой.\nТрансляция появится здесь, как только начнётся.'
            }
          />
          <StatsBar stats={stats} viewers={status?.viewers ?? 0} live={viewer.connected} />
        </section>
        {viewer.error && (
          <p className="error" role="alert">
            {viewer.error}
          </p>
        )}
        <div className="watch-note">
          <span className="tiny-dot green" />
          <p>Одна ссылка для всех эфиров. Сохраните страницу в закладки.</p>
        </div>
        <p className="watch-audio-hint">
          Громкость регулируется ползунком рядом с динамиком в плеере.
        </p>
      </main>
      <footer className="watch-footer">Sidecast · Ваш экран. Ваш сервер.</footer>
    </div>
  );
}
