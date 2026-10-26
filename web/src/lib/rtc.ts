import type { StreamSettings, StreamStats } from '../types';
import { AUDIO_BITRATE, GAME_AUDIO_CONSTRAINTS, withGameAudio } from './audio';

export async function offer(pc: RTCPeerConnection): Promise<RTCSessionDescriptionInit> {
  const done = new Promise<void>((resolve, reject) => {
    const cleanup = () => {
      clearTimeout(timer);
      pc.removeEventListener('icegatheringstatechange', check);
      pc.removeEventListener('connectionstatechange', closed);
    };

    const check = () => {
      if (pc.iceGatheringState === 'complete') {
        cleanup();
        resolve();
      }
    };

    const closed = () => {
      if (pc.connectionState === 'closed') {
        cleanup();
        reject(new Error('Подключение остановлено.'));
      }
    };

    const timer = setTimeout(() => {
      cleanup();
      reject(new Error('Сеть не ответила вовремя. Проверьте доступность STUN/TURN.'));
    }, 12000);

    pc.addEventListener('icegatheringstatechange', check);
    pc.addEventListener('connectionstatechange', closed);
  });

  void done.catch(() => {});
  await pc.setLocalDescription(withGameAudio(await pc.createOffer()));
  await done;

  return pc.localDescription!.toJSON();
}

export async function capture(settings: StreamSettings) {
  if (!window.isSecureContext || !navigator.mediaDevices?.getDisplayMedia)
    throw new Error('Для захвата экрана откройте студию через localhost на компьютере ведущего.');

  const options = {
    video: { frameRate: { ideal: settings.fps, max: settings.fps }, displaySurface: 'window' },
    audio: settings.audio === 'off' ? false : GAME_AUDIO_CONSTRAINTS,
    windowAudio: settings.audio === 'system' ? 'system' : 'window',
    systemAudio: settings.audio === 'off' ? 'exclude' : 'include',
    selfBrowserSurface: 'exclude',
  } as DisplayMediaStreamOptions;
  const stream = await navigator.mediaDevices.getDisplayMedia(options);

  for (const track of stream.getVideoTracks()) track.contentHint = 'motion';

  for (const track of stream.getAudioTracks()) {
    track.contentHint = 'music';
    await track.applyConstraints(GAME_AUDIO_CONSTRAINTS).catch(() => {});
  }

  return stream;
}

export async function tune(pc: RTCPeerConnection, bitrate: number, fps: number) {
  for (const sender of pc.getSenders()) {
    const p = sender.getParameters();

    if (!p.encodings?.length) continue;

    if (sender.track?.kind === 'video') p.degradationPreference = 'maintain-framerate';

    for (const e of p.encodings) {
      e.maxBitrate = sender.track?.kind === 'video' ? bitrate * 1_000_000 : AUDIO_BITRATE;

      if (sender.track?.kind === 'video') e.maxFramerate = fps;
    }

    await sender.setParameters(p).catch(() => {});
  }
}

export async function stats(
  pc: RTCPeerConnection,
  outbound: boolean,
  last: Map<string, { bytes: number; timestamp: number }>,
): Promise<StreamStats> {
  const report = await pc.getStats();
  const result: StreamStats = {};

  for (const item of report.values()) {
    if (
      item.type === 'candidate-pair' &&
      item.state === 'succeeded' &&
      item.nominated &&
      item.currentRoundTripTime != null
    )
      result.rtt = Math.round(item.currentRoundTripTime * 1000);

    if (item.type !== (outbound ? 'outbound-rtp' : 'inbound-rtp') || item.kind !== 'video')
      continue;

    const bytes = outbound ? item.bytesSent : item.bytesReceived;
    const prev = last.get(item.id);

    if (prev && item.timestamp > prev.timestamp)
      result.bitrate = Math.max(
        0,
        ((bytes - prev.bytes) * 8) / (item.timestamp - prev.timestamp) / 1000,
      );

    last.set(item.id, { bytes, timestamp: item.timestamp });
    result.fps = item.framesPerSecond;
    result.width = item.frameWidth;
    result.height = item.frameHeight;
    result.codec = report.get(item.codecId)?.mimeType?.replace('video/', '');
  }

  return result;
}
