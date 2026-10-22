import { useEffect, useState } from 'react';
import { api, channelPath, errorMessage } from '../lib/api';
import { offer } from '../lib/rtc';
import type { Negotiation, ServerConfig } from '../types';

export function useViewer(
  nickname: string,
  session: string,
  live: boolean,
  config: ServerConfig | null,
) {
  const [stream, setStream] = useState<MediaStream | null>(null);
  const [pc, setPC] = useState<RTCPeerConnection | null>(null);
  const [error, setError] = useState('');
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    setStream(null);
    setPC(null);
    setConnected(false);
    setError('');

    if (!live || !config || !session) return;

    let active = true;
    let peer: RTCPeerConnection | null = null;
    let subscription: Negotiation | null = null;
    let timer: ReturnType<typeof setTimeout>;

    const unsubscribe = (value: Negotiation | null) => {
      if (value?.viewerId)
        void api(`${channelPath(nickname)}/subscribe/${value.sessionId}/${value.viewerId}`, {
          method: 'DELETE',
          keepalive: true,
        }).catch(() => {});
    };

    const release = () => {
      peer?.close();
      peer = null;
      unsubscribe(subscription);
      subscription = null;
    };

    async function connect() {
      if (!active) return;

      release();

      const current = new RTCPeerConnection({ iceServers: config!.iceServers });

      peer = current;

      const media = new MediaStream();

      setPC(current);
      setConnected(false);
      current.addTransceiver('video', { direction: 'recvonly' });
      current.addTransceiver('audio', { direction: 'recvonly' });
      current.ontrack = (event) => {
        media.addTrack(event.track);

        try {
          if ('jitterBufferTarget' in event.receiver)
            (event.receiver as RTCRtpReceiver & { jitterBufferTarget: number }).jitterBufferTarget =
              120;
        } catch {
          /* Optional browser hint. */
        }

        if (active) setStream(media);
      };
      current.onconnectionstatechange = () => {
        if (!active || current !== peer) return;

        if (current.connectionState === 'connected') {
          setConnected(true);
          setError('');
        }

        if (current.connectionState === 'failed' || current.connectionState === 'disconnected') {
          setConnected(false);
          setError('Восстанавливаем соединение…');
          clearTimeout(timer);
          timer = setTimeout(connect, 3000);
        }
      };

      try {
        const description = await offer(current);

        if (!active) return;

        const result = await api<Negotiation>(`${channelPath(nickname)}/subscribe`, {
          method: 'POST',
          body: JSON.stringify({ description }),
        });

        if (!active || peer !== current) {
          unsubscribe(result);

          return;
        }

        subscription = result;
        await current.setRemoteDescription(result.description);

        if (active) setError('');
      } catch (e) {
        if (!active || peer !== current) return;

        setError(errorMessage(e));
        release();
        timer = setTimeout(connect, 4000);
      }
    }

    const pagehide = () => release();

    window.addEventListener('pagehide', pagehide);
    void connect();

    return () => {
      active = false;
      clearTimeout(timer);
      release();
      window.removeEventListener('pagehide', pagehide);
    };
  }, [nickname, session, live, config]);

  return { stream, pc, error, connected };
}
