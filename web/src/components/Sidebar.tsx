import { CircleHelp, Radio } from 'lucide-react';
import { Brand } from './Brand';

export function Sidebar({
  page,
  onPage,
}: {
  page: string;
  onPage: (page: 'studio' | 'help') => void;
}) {
  return (
    <aside className="sidebar">
      <Brand />
      <nav aria-label="Основная навигация">
        <button
          className={`nav-item ${page === 'studio' ? 'active' : ''}`}
          onClick={() => onPage('studio')}
        >
          <Radio size={19} /> Студия <span className="nav-dot" />
        </button>
        <button
          className={`nav-item ${page === 'help' ? 'active' : ''}`}
          onClick={() => onPage('help')}
        >
          <CircleHelp size={19} /> Как это работает
        </button>
      </nav>
    </aside>
  );
}
