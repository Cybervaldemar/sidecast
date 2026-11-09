import { useEffect, useRef, useState } from 'react';
import { Maximize, Minimize, MonitorPlay } from 'lucide-react';

export function VideoStage({
  stream,
  preview = false,
  waiting,
  title,
  subtitle,
  live = false,
  connectionError,
}: {
  stream: MediaStream | null;
  preview?: boolean;
  waiting?: boolean;
  title?: string;
  subtitle?: string;
  live?: boolean;
  connectionError?: string;
}) {
  const video = useRef<HTMLVideoElement>(null);
  const container = useRef<HTMLDivElement>(null);

  const [error, setError] = useState('');
  const [fullscreen, setFullscreen] = useState(false);

  useEffect(() => {
    const update = () => setFullscreen(document.fullscreenElement === container.current);

    update();
    document.addEventListener('fullscreenchange', update);

    return () => document.removeEventListener('fullscreenchange', update);
  }, []);

  useEffect(() => {
    if (!video.current) return;

    video.current.srcObject = stream;
    setError('');

    if (stream)
      void video.current
        .play()
        .catch(() => setError('Нажмите кнопку воспроизведения, чтобы начать просмотр.'));
    else if (!preview && document.fullscreenElement === video.current)
      void document.exitFullscreen().catch(() => {});
  }, [stream, preview]);

  async function toggleFullscreen() {
    if (!container.current) return;

    const exiting = document.fullscreenElement === container.current;

    try {
      if (exiting) await document.exitFullscreen();
      else await container.current.requestFullscreen();

      setError('');
    } catch {
      setError(
        exiting ? 'Не удалось выйти из полноэкранного режима.' : 'Полноэкранный режим недоступен.',
      );
    }
  }

  const message = error || connectionError;

  return (
    <div className={`video-stage ${stream ? 'has-stream' : ''}`} ref={container}>
      {preview && (
        <div className="stage-label" role="status">
          ПРЕДПРОСМОТР
        </div>
      )}
      <video
        ref={video}
        autoPlay
        playsInline
        muted
        controls={!!stream && !preview}
        onPlay={() => setError('')}
        aria-label={preview ? 'Предпросмотр трансляции' : 'Видеотрансляция'}
      />
      {!stream && (
        <div className="stage-empty">
          <div className={`stage-empty-icon ${waiting ? 'waiting' : ''}`}>
            <MonitorPlay size={32} strokeWidth={1.4} />
          </div>
          <h3>{title ?? 'Здесь появится ваш экран'}</h3>
          <p>{subtitle ?? 'Выберите окно, вкладку или весь экран,\nчтобы поделиться моментом.'}</p>
          {waiting && (
            <span className="waiting-label">
              <span className="tiny-dot" /> Подключимся автоматически
            </span>
          )}
        </div>
      )}
      {preview && (
        <div className="stage-controls">
          <span>
            {stream
              ? 'Локальный предпросмотр · звук выключен'
              : live
                ? 'Просмотр доступен по ссылке зрителя'
                : 'Готовы, когда будете готовы вы'}
          </span>
          <div>
            <button
              className="icon-button"
              disabled={!stream && !fullscreen}
              onClick={() => void toggleFullscreen()}
              aria-label={fullscreen ? 'Выйти из полного экрана' : 'Полный экран'}
              aria-pressed={fullscreen}
              title={fullscreen ? 'Выйти из полного экрана' : 'Полный экран'}
            >
              {fullscreen ? <Minimize size={18} /> : <Maximize size={18} />}
            </button>
          </div>
        </div>
      )}
      {message && (
        <div className="video-error" role="alert">
          {message}
        </div>
      )}
    </div>
  );
}
