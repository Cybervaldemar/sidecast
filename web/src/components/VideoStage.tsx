import { useEffect, useRef, useState } from 'react';
import { Maximize, Minimize, MonitorPlay, PictureInPicture2, Volume2, VolumeX } from 'lucide-react';

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
  const lastAudibleVolume = useRef(1);

  const [muted, setMuted] = useState(true);
  const [volume, setVolume] = useState(1);
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
        .catch(() => setError('Нажмите на видео, чтобы начать воспроизведение.'));
  }, [stream]);

  async function changeVolume(nextVolume: number) {
    if (!video.current) return;

    if (nextVolume > 0) lastAudibleVolume.current = nextVolume;

    video.current.volume = nextVolume;
    video.current.muted = nextVolume === 0;
    setVolume(nextVolume);
    setMuted(nextVolume === 0);

    try {
      await video.current.play();
      setError('');
    } catch {
      setError('Браузер не разрешил воспроизведение. Нажмите на видео.');
    }
  }

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
      <div className={`stage-label ${live ? 'is-live' : ''}`} role="status">
        <span className={`status-dot ${live ? 'live' : ''}`} aria-hidden="true" />
        {live ? 'В эфире' : preview ? 'ПРЕДПРОСМОТР' : 'Не в эфире'}
      </div>
      <video
        ref={video}
        autoPlay
        playsInline
        muted={preview || muted}
        onClick={() => {
          void video.current
            ?.play()
            .then(() => setError(''))
            .catch(() => {});
        }}
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
      {(preview || stream || fullscreen) && (
        <div className="stage-controls">
          {preview ? (
            <span>
              {stream
                ? 'Локальный предпросмотр · звук выключен'
                : live
                  ? 'Просмотр доступен по ссылке зрителя'
                  : 'Готовы, когда будете готовы вы'}
            </span>
          ) : stream ? (
            <div className="volume-control">
              <button
                className="icon-button"
                onClick={() =>
                  void changeVolume(muted || volume === 0 ? lastAudibleVolume.current : 0)
                }
                aria-label={muted ? 'Включить звук' : 'Выключить звук'}
              >
                {muted ? <VolumeX size={18} /> : <Volume2 size={18} />}
              </button>
              <input
                type="range"
                min={0}
                max={100}
                step={1}
                value={muted ? 0 : Math.round(volume * 100)}
                onChange={(event) => void changeVolume(Number(event.target.value) / 100)}
                aria-label="Громкость"
                aria-valuetext={`${muted ? 0 : Math.round(volume * 100)}%`}
                title="Громкость"
              />
            </div>
          ) : (
            <span />
          )}
          <div>
            {stream && !preview && 'requestPictureInPicture' in HTMLVideoElement.prototype && (
              <button
                className="icon-button"
                onClick={() => {
                  void video.current
                    ?.requestPictureInPicture()
                    .catch(() => setError('Режим «картинка в картинке» недоступен.'));
                }}
                aria-label="Картинка в картинке"
              >
                <PictureInPicture2 size={18} />
              </button>
            )}
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
