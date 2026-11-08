import { Activity, Gauge, Monitor } from 'lucide-react';
import type { StreamStats } from '../types';

export function StatsBar({ stats, live }: { stats: StreamStats; live: boolean }) {
  const items = [
    {
      icon: Monitor,
      label: 'Видео',
      value: stats.height ? `${stats.height}p` : '—',
      unit: stats.fps != null ? `${Math.round(stats.fps)} FPS` : 'ожидание',
    },
    {
      icon: Gauge,
      label: 'Битрейт',
      value: stats.bitrate != null ? stats.bitrate.toFixed(1) : '—',
      unit: 'Мбит/с',
    },
    {
      icon: Activity,
      label: 'Сетевой пинг',
      value: stats.rtt != null ? String(stats.rtt) : '—',
      unit: live ? 'мс · до сервера' : 'нет соединения',
    },
  ];

  return (
    <div className="stats-bar">
      {items.map(({ icon: Icon, label, value, unit }) => (
        <div className="stat" key={label}>
          <div className="stat-label">
            <Icon size={14} />
            {label}
          </div>
          <div className="stat-value">
            {value}
            <span>{unit}</span>
          </div>
        </div>
      ))}
    </div>
  );
}
