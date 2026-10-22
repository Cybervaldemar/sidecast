import { useEffect, useState } from 'react';
import { stats } from '../lib/rtc';
import type { StreamStats } from '../types';

export function usePeerStats(pc: RTCPeerConnection | null, outbound: boolean) {
  const [value, setValue] = useState<StreamStats>({});

  useEffect(() => {
    setValue({});

    if (!pc) return;

    let active = true;
    const last = new Map<string, { bytes: number; timestamp: number }>();
    const timer = setInterval(() => {
      void stats(pc, outbound, last)
        .then((next) => {
          if (active) setValue(next);
        })
        .catch(() => {});
    }, 1000);

    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [pc, outbound]);

  return value;
}
