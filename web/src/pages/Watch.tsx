import { Users } from 'lucide-react';
import { Brand } from '../components/Brand';
import { VideoStage } from '../components/VideoStage';
import { useChannelStatus } from '../hooks/useChannelStatus';
import { useViewer } from '../hooks/useViewer';
import { usePeerStats } from '../hooks/usePeerStats';
import type { ServerConfig } from '../types';

export function Watch({ nickname, config }: { nickname: string; config: ServerConfig | null }) {
  const { status, reachable } = useChannelStatus(nickname);
  const viewer = useViewer(nickname, status?.sessionId ?? '', !!status?.live, config);
  const stats = usePeerStats(viewer.pc, false);
  const live = !!status?.live && (reachable || viewer.connected);

  return (
    <div className="watch-page">
      <header className="watch-header">
        <Brand />
      </header>
      <main className="watch-content">
        <section className="watch-player" aria-label={`Трансляция ${nickname}`}>
          <VideoStage
            stream={viewer.stream}
            live={live}
            waiting={!status?.live}
            connectionError={viewer.error}
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
                  : 'Трансляция появится здесь, как только начнётся.'
            }
          />
        </section>
        <div className="watch-channel">
          <h1 title={nickname}>{nickname}</h1>
          {viewer.connected && (
            <div className="watch-details">
              <span className="watch-viewers" aria-label={`Зрителей: ${status?.viewers ?? 0}`}>
                <Users size={15} aria-hidden="true" />
                {status?.viewers ?? 0}
              </span>
              {!!stats.height && <span>{stats.height}p</span>}
              {!!stats.fps && <span>{Math.round(stats.fps)} FPS</span>}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
