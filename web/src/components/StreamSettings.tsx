import { Monitor, SlidersHorizontal, Volume2 } from 'lucide-react';
import type { StreamSettings as Settings } from '../types';

export function StreamSettings({
  value,
  onChange,
  disabled,
}: {
  value: Settings;
  onChange: (value: Settings) => void;
  disabled: boolean;
}) {
  return (
    <section className="card settings-card">
      <div className="section-heading">
        <span className="section-icon">
          <SlidersHorizontal size={18} />
        </span>
        <h2>Настройки эфира</h2>
      </div>
      <fieldset disabled={disabled}>
        <label className="setting-label">
          <Monitor size={15} /> Частота кадров
        </label>
        <div className="segmented" role="group" aria-label="Частота кадров">
          {[30, 60, 120].map((fps) => (
            <button
              type="button"
              key={fps}
              aria-pressed={value.fps === fps}
              className={value.fps === fps ? 'selected' : ''}
              onClick={() => onChange({ ...value, fps })}
            >
              {fps}
              <span>FPS</span>
            </button>
          ))}
        </div>
        <label className="setting-label" htmlFor="bitrate">
          Максимальный битрейт
        </label>
        <select
          id="bitrate"
          value={value.bitrate}
          onChange={(e) => onChange({ ...value, bitrate: Number(e.target.value) })}
        >
          <option value={6}>6 Мбит/с · экономный</option>
          <option value={12}>12 Мбит/с · сбалансированный</option>
          <option value={20}>20 Мбит/с · высокое качество</option>
          <option value={40}>40 Мбит/с · максимум деталей</option>
        </select>
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
        <label className="setting-label">
          <Volume2 size={15} /> Передавать звук
        </label>
        <div className="segmented" role="group" aria-label="Передавать звук">
          <button
            type="button"
            aria-pressed={value.audio}
            className={value.audio ? 'selected' : ''}
            onClick={() => onChange({ ...value, audio: true })}
          >
            Со звуком
          </button>
          <button
            type="button"
            aria-pressed={!value.audio}
            className={!value.audio ? 'selected' : ''}
            onClick={() => onChange({ ...value, audio: false })}
          >
            Без звука
          </button>
        </div>
        <details className="settings-help">
          <summary>О видео и звуке</summary>
          <p className="field-hint">
            Разрешение — как у выбранного источника. По умолчанию сохраняем плавность: при нехватке
            скорости или ресурсов браузер может уменьшить размер кадра. Режим «Сохранять разрешение»
            удерживает размер кадра, но может заметно снижать FPS.
          </p>
          <p className="field-hint">
            Для динамичных игр в 1440p начните с 20 Мбит/с и 60 FPS. Лимит битрейта — верхняя
            граница: фактическая скорость автоматически подстраивается под сеть. Меньший лимит
            экономит трафик, но оставляет меньше деталей в движении.
          </p>
          <p className="field-hint">
            Доступны лимиты до 40 Мбит/с. Фактический битрейт зависит от сцены, кодировщика и сети;
            серверу нужен запас исходящей скорости для каждого зрителя.
          </p>
          <p className="field-hint">
            Если звук включён здесь, выберите его передачу в диалоге браузера при запуске. Доступные
            источники звука зависят от браузера и ОС.
          </p>
          <p className="field-hint">
            Если смотрите свой эфир на том же компьютере, выключите звук в окне зрителя: при захвате
            системного звука воспроизведение может снова попасть в эфир.
          </p>
        </details>
      </fieldset>
    </section>
  );
}
