// System/game audio needs the full stereo signal, without microphone processing.
// These are preferences: a mono source must still be capturable.
export const GAME_AUDIO_CONSTRAINTS: MediaTrackConstraints = {
  channelCount: { ideal: 2 },
  sampleRate: { ideal: 48_000 },
  echoCancellation: false,
  noiseSuppression: false,
  autoGainControl: false,
};

export const AUDIO_BITRATE = 256_000;

// Chrome configures its Opus decoder from the local receive preferences. The
// server's stereo answer alone still leaves a default offer downmixed to mono.
// Codec preferences expose no API for changing Opus format parameters, so edit
// only the negotiated Opus lines before installing the local offer.
export function withGameAudio(description: RTCSessionDescriptionInit): RTCSessionDescriptionInit {
  if (!description.sdp) return description;

  const newline = description.sdp.includes('\r\n') ? '\r\n' : '\n';
  const preferences = [
    'stereo=1',
    'sprop-stereo=1',
    `maxaveragebitrate=${AUDIO_BITRATE}`,
    'maxplaybackrate=48000',
    'useinbandfec=1',
    'usedtx=0',
  ];
  const replaced = new Set(preferences.map((parameter) => parameter.split('=')[0]));
  const sdp = description.sdp
    .split(/(?=^m=)/m)
    .map((section) => {
      if (!section.startsWith('m=audio ')) return section;

      const lines = section.split(/\r?\n/);
      const payloads = lines.flatMap((line) => {
        const match = /^a=rtpmap:(\d+) opus\/48000\/2$/i.exec(line);

        return match ? [match[1]] : [];
      });

      for (const payload of payloads) {
        const prefix = `a=fmtp:${payload} `;
        const index = lines.findIndex((line) => line.startsWith(prefix));
        const existing = index === -1 ? [] : lines[index].slice(prefix.length).split(';');
        const preserved = existing
          .map((parameter) => parameter.trim())
          .filter((parameter) => parameter && !replaced.has(parameter.split('=')[0].toLowerCase()));
        const line = `${prefix}${[...preserved, ...preferences].join(';')}`;

        if (index === -1)
          lines.splice(lines.at(-1) === '' ? lines.length - 1 : lines.length, 0, line);
        else lines[index] = line;
      }

      return lines.join(newline);
    })
    .join('');

  return { ...description, sdp };
}
