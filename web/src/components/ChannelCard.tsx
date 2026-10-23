import { useState } from 'react';
import { ArrowRight, Check, Copy, Download, ExternalLink, KeyRound, Link2 } from 'lucide-react';
import { claimChannel, errorMessage } from '../lib/api';
import type { Identity, ServerConfig } from '../types';

export function shareOrigin(config: ServerConfig | null, address: string) {
  if (config?.publicUrl) return config.publicUrl;

  const current = new URL(window.location.origin);

  if (
    address &&
    (current.hostname === 'localhost' ||
      current.hostname === '127.0.0.1' ||
      current.hostname === '[::1]' ||
      config?.addresses.includes(current.hostname.replace(/^\[|\]$/g, '')))
  )
    current.hostname = address.includes(':') ? `[${address}]` : address;

  return current.origin;
}

export function ChannelCard({
  identity,
  config,
  onIdentity,
  disabled,
}: {
  identity: Identity | null;
  config: ServerConfig | null;
  onIdentity: (identity: Identity) => void;
  disabled: boolean;
}) {
  const [nickname, setNickname] = useState('');
  const [key, setKey] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  const [address, setAddress] = useState('');

  const hostname = location.hostname.replace(/^\[|\]$/g, '');
  const localAddress = config?.addresses.includes(hostname);
  const selectedAddress = config?.addresses.includes(address)
    ? address
    : localAddress
      ? hostname
      : config?.addresses[0] || '';
  const origin = shareOrigin(config, selectedAddress);
  const link = identity ? `${origin}/watch/${identity.nickname}` : '';

  async function claim(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError('');

    try {
      onIdentity(await claimChannel(nickname.toLowerCase().trim(), key.trim() || undefined));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
      setTimeout(() => setCopied(false), 2200);
    } catch {
      setError('Выделите и скопируйте ссылку из поля вручную.');
    }
  }

  function backup() {
    const blob = new Blob([JSON.stringify(identity, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');

    anchor.href = url;
    anchor.download = `sidecast-${identity!.nickname}-key.json`;
    anchor.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  return (
    <section className="card channel-card">
      <div className="section-heading">
        <span className="section-icon">
          <Link2 size={19} />
        </span>
        <h2>Канал</h2>
      </div>
      {identity ? (
        <>
          <div className="share-row">
            <input
              aria-label="Постоянная ссылка"
              readOnly
              value={link}
              onFocus={(e) => e.target.select()}
            />
            <button className="button secondary" onClick={copy}>
              {copied ? <Check size={16} /> : <Copy size={16} />}
              {copied ? 'Скопировано' : 'Копировать'}
            </button>
            <a
              className="icon-button"
              href={`/watch/${identity.nickname}`}
              target="_blank"
              rel="noreferrer"
              aria-label="Открыть канал"
            >
              <ExternalLink size={17} />
            </a>
          </div>
          {!config?.publicUrl &&
            (localAddress ||
              hostname === 'localhost' ||
              hostname === '127.0.0.1' ||
              hostname === '::1') &&
            !!config &&
            config.addresses.length > 1 && (
              <label className="network-select">
                Адрес для зрителей
                <select value={selectedAddress} onChange={(e) => setAddress(e.target.value)}>
                  {config.addresses.map((ip) => (
                    <option key={ip} value={ip}>
                      {ip}
                    </option>
                  ))}
                </select>
              </label>
            )}
          <details className="key-details">
            <summary>
              <KeyRound size={14} /> Ключ владельца и восстановление
            </summary>
            <p>
              Сохраните ключ, чтобы вернуться к каналу в другом браузере. Он разрешает вести эфир —
              храните его у себя.
            </p>
            <button className="button secondary" onClick={backup}>
              <Download size={15} /> Сохранить ключ
            </button>
            <label>
              Ключ владельца
              <input
                readOnly
                type="password"
                value={identity.token}
                onFocus={(e) => e.target.select()}
              />
            </label>
          </details>
        </>
      ) : (
        <>
          <p className="muted">Выберите никнейм — он станет адресом вашей трансляции.</p>
          <form onSubmit={claim} className="claim-form">
            <label htmlFor="nickname">Никнейм</label>
            <div className="claim-row">
              <span className="input-prefix">/watch/</span>
              <input
                id="nickname"
                autoComplete="username"
                placeholder="your-nickname"
                value={nickname}
                onChange={(e) => setNickname(e.target.value.toLowerCase())}
                pattern="[a-z0-9][a-z0-9_\-]{2,31}"
                minLength={3}
                maxLength={32}
                required
                disabled={busy || disabled}
              />
              <button className="button primary" disabled={busy || disabled}>
                {busy ? 'Создаём…' : 'Создать канал'}
                <ArrowRight size={16} />
              </button>
            </div>
            <span className="field-hint">3–32 символа: a–z, 0–9, дефис, подчёркивание</span>
            <details className="key-details">
              <summary>
                <KeyRound size={14} /> У меня уже есть канал
              </summary>
              <label>
                Ключ владельца
                <input
                  type="password"
                  autoComplete="off"
                  value={key}
                  onChange={(e) => setKey(e.target.value)}
                  placeholder="Вставьте ключ из сохранённого файла"
                />
              </label>
              <p>Введите свой никнейм выше и нажмите «Создать канал», чтобы восстановить доступ.</p>
            </details>
          </form>
        </>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
