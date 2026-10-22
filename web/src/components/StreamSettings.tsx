import { Monitor, SlidersHorizontal, Volume2 } from 'lucide-react';
import type { NativeCapabilities, StreamSettings as Settings } from '../types';

export function StreamSettings({
  value,
  onChange,
  disabled,
  native,
}: {
  value: Settings;
  onChange: (value: Settings) => void;
  disabled: boolean;
  native?: NativeCapabilities;
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
        <label className="setting-label" htmlFor="audio">
          <Volume2 size={15} /> Источник звука
        </label>
        <select
          id="audio"
          value={value.audio}
          onChange={(e) => onChange({ ...value, audio: e.target.value as Settings['audio'] })}
        >
          <option value="window" disabled={native && !native.applicationAudio}>
            {native ? 'Звук выбранного приложения' : 'Звук выбранного окна'}
          </option>
          <option value="system" disabled={native && !native.audio}>
            Системный звук
          </option>
          <option value="off">Без звука</option>
        </select>
        <details className="settings-help">
          <summary>О видео и звуке</summary>
          <p className="field-hint">
            60 FPS — для плавных игр и рабочего стола. Разрешение — как у выбранного источника.
          </p>
          <p className="field-hint">
            {native
              ? 'Системный звук захватывает все приложения на основном устройстве вывода; без звука — только видео.'
              : 'Включите передачу звука в диалоге выбора экрана. Доступность зависит от браузера и ОС.'}
          </p>
        </details>
      </fieldset>
    </section>
  );
}
