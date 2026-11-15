import { useCallback, useEffect, useRef, useState } from 'react';
import { api, channelPath, errorMessage } from '../lib/api';
import { capture, offer, tune } from '../lib/rtc';
import { nativeCapture, type NativeStatus } from '../lib/native';
import { useViewer } from './useViewer';
import type { Identity, Negotiation, ServerConfig, StreamSettings } from '../types';

interface Active {
  pc: RTCPeerConnection;
  stream: MediaStream;
  session?: string;
  identity: Identity;
  targetTimer?: number;
  targetBusy?: boolean;
  videoBitrate?: number;
}

interface NativeActive {
  session: string;
  timer: number;
}

export function usePublisher(identity: Identity | null, config: ServerConfig | null) {
  const active = useRef<Active | null>(null);
  const generation = useRef(0);
  const locked = useRef(false);
  const nativeActive = useRef<NativeActive | null>(null);

  const [stream, setStream] = useState<MediaStream | null>(null);
  const [pc, setPC] = useState<RTCPeerConnection | null>(null);
  const [phase, setPhase] = useState<'idle' | 'starting' | 'live'>('idle');
  const [error, setError] = useState('');
  const [nativeSession, setNativeSession] = useState('');
  const [nativeStatus, setNativeStatus] = useState<NativeStatus | null>(null);
  const viewer = useViewer(identity?.nickname ?? '', nativeSession, !!nativeSession, config);

  const stop = useCallback(() => {
    generation.current++;

    const current = active.current;

    active.current = null;
    const currentNative = nativeActive.current;
    nativeActive.current = null;
    if (currentNative) {
      window.clearInterval(currentNative.timer);
      void nativeCapture()
        ?.Stop()
        .catch(() => {});
    }

    if (current) {
      window.clearInterval(current.targetTimer);
      current.pc.close();
      current.stream.getTracks().forEach((track) => track.stop());

      if (current.session)
        void api(
          `${channelPath(current.identity.nickname)}/publish/${current.session}`,
          { method: 'DELETE', keepalive: true },
          current.identity.token,
        ).catch(() => {});
    }

    setPC(null);
    setStream(null);
    setNativeSession('');
    setNativeStatus(null);
    setPhase('idle');
  }, []);

  useEffect(() => {
    const cleanup = () => stop();

    window.addEventListener('pagehide', cleanup);

    return () => {
      window.removeEventListener('pagehide', cleanup);
      stop();
    };
  }, [stop]);

  const start = async (settings: StreamSettings) => {
    if (!identity || !config || locked.current || active.current || nativeActive.current) return;

    locked.current = true;

    const revision = ++generation.current;

    setError('');
    setPhase('starting');

    let current: Active | null = null;

    try {
      const native = nativeCapture();
      if (native) {
        if (!settings.sourceId) throw new Error('Выберите монитор или окно для трансляции.');
        const session = await native.Start(
          identity.nickname,
          identity.token,
          settings.sourceId,
          settings.includeAudio,
        );
        if (revision !== generation.current) {
          await native.Stop();
          return;
        }
        const refresh = async () => {
          const status = await native.Status();
          if (revision !== generation.current) return;
          if (status.sessionId !== session) {
            stop();
            setError(status.error || 'Нативный захват остановился. Запустите эфир снова.');
          } else setNativeStatus(status);
        };
        nativeActive.current = {
          session,
          timer: window.setInterval(() => void refresh().catch(() => {}), 1000),
        };
        setNativeSession(session);
        setPhase('live');
        void refresh().catch(() => {});
        return;
      }
      const media = await capture(settings);

      if (revision !== generation.current) {
        media.getTracks().forEach((track) => track.stop());

        return;
      }

      const peer = new RTCPeerConnection({ iceServers: config.iceServers });

      current = { pc: peer, stream: media, identity };
      active.current = current;
      setStream(media);
      setPC(peer);
      media.getVideoTracks()[0].addEventListener('ended', stop, { once: true });

      for (const track of media.getTracks()) peer.addTrack(track, media);

      peer.onconnectionstatechange = () => {
        if (active.current !== current) return;

        if (peer.connectionState === 'connected') setPhase('live');

        if (peer.connectionState === 'failed') {
          stop();
          setError('Соединение потеряно. Запустите эфир снова — ссылка сохранена.');
        }
      };

      const description = await offer(peer);

      if (revision !== generation.current) return;

      const result = await api<Negotiation>(
        `${channelPath(identity.nickname)}/publish`,
        {
          method: 'POST',
          body: JSON.stringify({ description, audio: media.getAudioTracks().length > 0 }),
        },
        identity.token,
      );

      current.session = result.sessionId;

      if (revision !== generation.current) {
        void api(
          `${channelPath(identity.nickname)}/publish/${result.sessionId}`,
          { method: 'DELETE' },
          identity.token,
        ).catch(() => {});

        return;
      }

      await peer.setRemoteDescription(result.description);
      const publishing = current;
      if (!publishing) return;
      publishing.videoBitrate = config.maxVideoBitrate;
      await tune(peer, settings, publishing.videoBitrate);

      const refreshTarget = async () => {
        if (active.current !== publishing || !publishing.session || publishing.targetBusy) return;
        publishing.targetBusy = true;

        try {
          const { bitrate } = await api<{ bitrate: number }>(
            `${channelPath(identity.nickname)}/publish/${publishing.session}/target`,
            { signal: AbortSignal.timeout(3000) },
            identity.token,
          );
          if (active.current !== publishing || !Number.isFinite(bitrate) || bitrate < 500_000)
            return;
          if (
            Math.abs(bitrate - (publishing.videoBitrate ?? 0)) <
            (publishing.videoBitrate ?? 0) * 0.1
          )
            return;

          await tune(peer, settings, bitrate);
          publishing.videoBitrate = bitrate;
        } catch {
          // The sender's own WebRTC congestion control remains active.
        } finally {
          publishing.targetBusy = false;
        }
      };
      publishing.targetTimer = window.setInterval(() => void refreshTarget(), 3000);
      void refreshTarget();
    } catch (e) {
      if (revision === generation.current) {
        stop();
        setError(errorMessage(e));
      }
    } finally {
      locked.current = false;
    }
  };

  const native = !!nativeCapture();
  return {
    start,
    stop,
    stream: native ? viewer.stream : stream,
    pc: native ? viewer.pc : pc,
    phase,
    error,
    native,
    nativeStatus,
    hasAudio: native ? !!nativeStatus?.audio : !!stream?.getAudioTracks().length,
  };
}
