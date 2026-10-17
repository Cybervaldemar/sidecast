import type { ChannelStatus, Identity, ServerConfig } from '../types';
const messages: Record<string, string> = {
  nickname_taken: 'Этот никнейм уже занят. Выберите другой или восстановите ключ владельца.',
  invalid_nickname: 'Никнейм: 3–32 символа, латинские буквы, цифры, дефис и подчёркивание. Первый символ — буква или цифра.',
  owner_required: 'Ключ не подходит к этому каналу.', stream_busy: 'Канал уже ведёт эфир. Завершите его в другой вкладке.',
  stream_offline: 'Трансляция ещё не началась.', viewer_limit: 'Все места заняты. Подключение повторится автоматически.',
  negotiation_failed: 'Не удалось установить соединение. Проверьте настройки сети и повторите попытку.',
  server_busy: 'Сервер занят. Попробуйте немного позже.', channel_limit: 'Достигнут лимит каналов на сервере.',
  storage_error: 'Сервер не смог сохранить канал. Проверьте доступ к папке данных.',
};
export class APIError extends Error {
  constructor(public code: string, public status: number) { super(messages[code] ?? `Ошибка сервера (${status}).`); }
}
export async function api<T>(path: string, init: RequestInit = {}, secret?: string): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body) headers.set('Content-Type', 'application/json');
  if (secret) headers.set('Authorization', `Bearer ${secret}`);
  const response = await fetch(`/api${path}`, { ...init, headers, signal: init.signal ?? AbortSignal.timeout(20000) });
  if (!response.ok) { const body = await response.json().catch(() => ({})); if (body.error === 'native_capture_failed' && typeof body.message === 'string') throw new Error(body.message); throw new APIError(body.error ?? 'unknown', response.status); }
  return response.status === 204 ? undefined as T : response.json();
}
export const channelPath = (nick: string) => `/channels/${encodeURIComponent(nick)}`;
export const getConfig = () => api<ServerConfig>('/config');
export const getStatus = (nick: string, signal?: AbortSignal) => api<ChannelStatus>(channelPath(nick), { signal });
export const claimChannel = (nickname: string, token?: string) => api<Identity>(channelPath(nickname), { method: 'PUT' }, token);
export function errorMessage(error: unknown): string {
  if (error instanceof DOMException && error.name === 'NotAllowedError') return 'Выбор экрана отменён или браузер не разрешил захват.';
  return error instanceof Error ? error.message : 'Не удалось выполнить действие.';
}
