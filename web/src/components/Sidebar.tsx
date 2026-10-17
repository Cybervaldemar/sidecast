import { ArrowUpRight, CircleHelp, Radio, ShieldCheck, Sparkles } from 'lucide-react';
import { Brand } from './Brand';
export function Sidebar({ page, onPage }: { page: string; onPage: (page: 'studio' | 'help') => void }) {
  return <aside className="sidebar">
    <Brand />
    <div className="workspace-label">ВАШЕ ПРОСТРАНСТВО</div>
    <nav aria-label="Основная навигация">
      <button className={`nav-item ${page === 'studio' ? 'active' : ''}`} onClick={() => onPage('studio')}><Radio size={19} /> Студия <span className="nav-dot" /></button>
      <button className={`nav-item ${page === 'help' ? 'active' : ''}`} onClick={() => onPage('help')}><CircleHelp size={19} /> Как это работает <ArrowUpRight size={14} className="nav-arrow" /></button>
    </nav>
    <div className="sidebar-bottom">
      <div className="privacy-note"><ShieldCheck size={23} /><strong>Ваш экран. Ваш сервер.</strong><p>Эфир в реальном времени.<br />Без записи и облачного аккаунта.</p></div>
      <div className="version"><span><Sparkles size={13} /> Новая версия</span><span>v2 alpha</span></div>
    </div>
  </aside>;
}
