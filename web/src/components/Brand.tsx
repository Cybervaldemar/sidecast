import { Play } from 'lucide-react';

export function Brand() {
  return (
    <a className="brand" href="/" aria-label="Sidecast — главная">
      <span className="brand-mark">
        <Play size={19} fill="currentColor" />
      </span>
      <span>
        sidecast<span className="brand-dot">.</span>
      </span>
    </a>
  );
}
