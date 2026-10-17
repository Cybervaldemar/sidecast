import { useEffect, useState } from 'react';
import { Monitor } from 'lucide-react';
import { api, errorMessage } from '../lib/api';
import type { NativeCapabilities, NativeOptions, NativeSource } from '../types';

export function CaptureMode({ capabilities, mode, onMode, options, onOptions, audio, disabled }: {
  capabilities?: NativeCapabilities; mode: 'browser' | 'native'; onMode: (mode: 'browser' | 'native') => void;
  options: NativeOptions; onOptions: (options: NativeOptions) => void; audio: string; disabled: boolean;
}) {
  const [sources, setSources] = useState<NativeSource[]>([]); const [error, setError] = useState('');
  async function refresh() {
    try { setError(''); const next = await api<NativeSource[]>('/native/sources'); setSources(next); }
    catch (e) { setError(errorMessage(e)); }
  }
  useEffect(() => { if (mode === 'native' && capabilities?.available) void refresh(); }, [mode, capabilities?.available]);
  const selected = sources.find(source => source.id === options.sourceId);
  return <section className="card settings-card capture-mode"><div className="section-heading"><span className="section-icon"><Monitor size={18} /></span><h2>Способ захвата</h2></div>
    <fieldset disabled={disabled}><label className="setting-label" htmlFor="capture-mode">Режим</label><select id="capture-mode" value={mode} onChange={event => onMode(event.target.value as 'browser' | 'native')}><option value="browser">Браузерный · видео и звук</option>{capabilities?.available && <option value="native">Нативный · Windows</option>}</select>
    <p className="field-hint">{mode === 'native' ? 'Захват средствами Windows. Для 120 FPS нужны подходящий источник, видеокарта и сеть.' : capabilities?.reason ?? 'Видео и звук захватывает браузер. Нативный режим Windows доступен при запуске LANPlay Studio.'}</p>
    {mode === 'native' && <><label className="setting-label" htmlFor="native-source">Окно или экран</label><select id="native-source" value={options.sourceId} onChange={event => {
      const source = sources.find(item => item.id === event.target.value);
      onOptions({ ...options, sourceId: event.target.value, audioSourceId: source?.kind === 'window' ? source.id : options.audioSourceId });
    }}><option value="">Выберите источник</option>{sources.map(source => <option key={source.id} value={source.id}>{source.name}</option>)}</select>
    <button className="button secondary refresh-sources" type="button" onClick={() => void refresh()}>Обновить список</button>
    {audio === 'window' && <><label className="setting-label" htmlFor="native-audio-source">Приложение для звука</label><select id="native-audio-source" value={options.audioSourceId} disabled={!capabilities?.applicationAudio} onChange={event => onOptions({ ...options, audioSourceId: event.target.value })}><option value="">Выберите приложение</option>{sources.filter(source => source.kind === 'window').map(source => <option key={source.id} value={source.id}>{source.name}</option>)}</select><p className="field-hint">{capabilities?.applicationAudio ? 'Передаётся звук приложения и его дочерних процессов. Другие окна того же приложения могут входить в этот процесс.' : 'Звук приложения недоступен в этой версии Windows. Выберите системный звук, без звука или браузерный режим.'}</p></>}
    {selected?.kind === 'monitor' && audio === 'system' && <p className="field-hint">Передаётся весь экран и звук всех приложений на основном устройстве вывода.</p>}
    <label className="setting-label" htmlFor="native-encoder">Кодировщик</label><select id="native-encoder" value={options.encoder} onChange={event => onOptions({ ...options, encoder: event.target.value })}><option value="auto">Автоматически</option><option value="h264_nvenc">NVIDIA NVENC</option><option value="h264_amf">AMD AMF</option><option value="h264_qsv">Intel Quick Sync</option><option value="libx264">Программный H.264</option></select></>}
    </fieldset>{error && <p className="error" role="alert">{error}</p>}
  </section>;
}
