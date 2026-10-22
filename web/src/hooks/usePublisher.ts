import { useCallback, useEffect, useRef, useState } from 'react';
import { api, channelPath, errorMessage } from '../lib/api';
import { capture, offer, tune } from '../lib/rtc';
import type { Identity, Negotiation, ServerConfig, StreamSettings } from '../types';

interface Active {
  pc: RTCPeerConnection;
  stream: MediaStream;
  session?: string;
  identity: Identity;
}

export function usePublisher(identity: Identity | null, config: ServerConfig | null) {
  const active = useRef<Active | null>(null);
  const generation = useRef(0);
  const locked = useRef(false);

  const [stream, setStream] = useState<MediaStream | null>(null);
  const [pc, setPC] = useState<RTCPeerConnection | null>(null);
  const [phase, setPhase] = useState<'idle' | 'starting' | 'live'>('idle');
  const [error, setError] = useState('');

  const stop = useCallback(() => {
    generation.current++;

    const current = active.current;

    active.current = null;

    if (current) {
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
    if (!identity || !config || locked.current || active.current) return;

    locked.current = true;

    const revision = ++generation.current;

    setError('');
    setPhase('starting');

    let current: Active | null = null;

    try {
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
      await tune(peer, settings.bitrate, settings.fps);
    } catch (e) {
      if (revision === generation.current) {
        stop();
        setError(errorMessage(e));
      }
    } finally {
      locked.current = false;
    }
  };

  return { start, stop, stream, pc, phase, error };
}
