import { useEffect, useState } from 'react';
import { SlidersHorizontal, Volume2 } from 'lucide-react';
import type { StreamSettings as Settings } from '../types';
import { nativeCapture, type NativeSource } from '../lib/native';

export function StreamSettings({
  value,
  onChange,
  disabled,
}: {
  value: Settings;
  onChange: (value: Settings) => void;
  disabled: boolean;
}) {
  const native = nativeCapture();
  const [sources, setSources] = useState<NativeSource[]>([]);
  const [sourceError, setSourceError] = useState('');

  useEffect(() => {
    if (!native) return;
    let active = true;
    native
      .Sources()
      .then((items) => {
        if (!active) return;
        setSources(items);
        if (!value.sourceId && items.length) onChange({ ...value, sourceId: items[0].id });
      })
      .catch((error: unknown) => {
        if (active)
          setSourceError(error instanceof Error ? error.message : 'Не удалось найти источники');
      });
    return () => {
      active = false;
    };
  }, []);

  return (
    <section className="card settings-card">
      <div className="section-heading">
        <span className="section-icon">
          <SlidersHorizontal size={18} />
        </span>
        <h2>Настройки эфира</h2>
      </div>
      <fieldset disabled={disabled}>
        {native && (
          <>
            <label className="setting-label" htmlFor="native-source">
              Источник видео
            </label>
            <select
              id="native-source"
              value={value.sourceId}
              onChange={(e) => onChange({ ...value, sourceId: e.target.value })}
            >
              {!sources.length && <option value="">Источники не найдены</option>}
              {sources.map((source) => (
                <option key={source.id} value={source.id}>
                  {source.name}
                </option>
              ))}
            </select>
            {sourceError && <p className="field-hint error">{sourceError}</p>}
            <button
              type="button"
              className="button secondary"
              onClick={() =>
                void native
                  .Sources()
                  .then((items) => {
                    setSources(items);
                    if (!items.some((item) => item.id === value.sourceId))
                      onChange({ ...value, sourceId: items[0]?.id ?? '' });
                    setSourceError('');
                  })
                  .catch((error: unknown) =>
                    setSourceError(
                      error instanceof Error ? error.message : 'Не удалось обновить список',
                    ),
                  )
              }
            >
              Обновить список окон
            </button>
          </>
        )}
        {!native && (
          <>
            <label className="setting-label" htmlFor="video-priority">
              Приоритет видео
            </label>
            <select
              id="video-priority"
              value={value.videoPriority}
              onChange={(e) =>
                onChange({ ...value, videoPriority: e.target.value as Settings['videoPriority'] })
              }
            >
              <option value="resolution">Сохранять разрешение</option>
              <option value="motion">Сохранять плавность</option>
            </select>
          </>
        )}
        <label className="setting-label audio-checkbox">
          <input
            type="checkbox"
            checked={value.includeAudio}
            onChange={(e) => onChange({ ...value, includeAudio: e.target.checked })}
          />{' '}
          Передавать звук
        </label>
        {!native && (
          <>
            <div className="setting-label" id="audio-source-label">
              <Volume2 size={15} /> Источник звука
            </div>
            <div className="segmented" role="group" aria-labelledby="audio-source-label">
              <button
                type="button"
                aria-pressed={value.audioSource === 'window'}
                className={value.audioSource === 'window' ? 'selected' : ''}
                onClick={() => onChange({ ...value, audioSource: 'window' })}
              >
                Звук окна
              </button>
              <button
                type="button"
                aria-pressed={value.audioSource === 'system'}
                className={value.audioSource === 'system' ? 'selected' : ''}
                onClick={() => onChange({ ...value, audioSource: 'system' })}
              >
                Звук ПК
              </button>
            </div>
          </>
        )}
        {native ? (
          <p className="field-hint">
            Нативный захват передаёт общий звук основного устройства вывода Windows. Предпросмотр
            открыт без звука.
          </p>
        ) : (
          <p className="field-hint">
            В диалоге выбора источника отдельно включите или выключите передачу звука. Если
            переключатель выключен, эфир будет без аудио. «Звук ПК» может помочь убрать звуки
            DualSense, если они идут через отдельный выход.
          </p>
        )}
        <details className="settings-help">
          <summary>О видео и звуке</summary>
          <p className="field-hint">
            Разрешение — как у выбранного источника. Приложение стремится сохранить плавные 60 FPS и
            передать больше кадров, если экран, кодировщик и сеть это позволяют. При перегрузке
            частота кадров или разрешение могут снизиться.
          </p>
          <p className="field-hint">
            Битрейт подбирается автоматически с верхней границей 40 Мбит/с. Для каждого зрителя
            серверу нужен соответствующий запас исходящей скорости.
          </p>
          {!native && (
            <>
              <p className="field-hint">
                Выбор источника звука подсказывает системе захвата, какой звук предложить при выборе
                окна. Окончательное решение о передаче аудио вы принимаете в диалоге выбора
                источника. Для режима «Звук окна» выберите окно игры. Этот звук может включать
                сигналы DualSense, которые создаёт сама игра, даже если они выводятся через
                отдельное устройство. Браузер и ОС могут не поддерживать изоляцию звука окна и
                предложить системный звук или видео без звука.
              </p>
              <p className="field-hint">
                «Звук ПК» запрашивает системный звук: обычно это общий звук основного выхода,
                включая другие приложения. Звуки, направленные на отдельный аудиовыход DualSense,
                могут в него не попасть. При выборе окна браузер может предложить системный звук;
                доступность зависит от браузера и ОС.
              </p>
            </>
          )}
          <p className="field-hint">
            Если смотрите свой эфир на том же компьютере, выключите звук в окне зрителя: при захвате
            системного звука воспроизведение может снова попасть в эфир.
          </p>
        </details>
      </fieldset>
    </section>
  );
}
