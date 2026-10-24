if (navigator.mediaDevices && !location.pathname.startsWith('/watch/')) {
  let captureCalls = 0;
  Object.defineProperty(navigator.mediaDevices, 'getDisplayMedia', {
    configurable: true,
    value: async options => {
      captureCalls++;
      const scenario = new URL(location.href).searchParams.get('capture');
      if (scenario === 'cancel') throw new DOMException('Test cancellation', 'NotAllowedError');
      const canvas = document.createElement('canvas'); canvas.width = 1920; canvas.height = 1080;
      const context = canvas.getContext('2d');
      let texture;
      if (scenario === 'motion') {
        texture = document.createElement('canvas'); texture.width = 2560; texture.height = 1440;
        const pixels = texture.getContext('2d');
        let seed = 12345;
        for (let y = 0; y < texture.height; y += 16) for (let x = 0; x < texture.width; x += 16) {
          seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
          pixels.fillStyle = `rgb(${seed & 255},${(seed >>> 8) & 255},${(seed >>> 16) & 255})`;
          pixels.fillRect(x, y, 16, 16);
        }
      }
      let frame = 0;
      const draw = () => {
        frame++;
        context.fillStyle = '#11151f'; context.fillRect(0, 0, 1920, 1080);
        if (texture) {
          context.drawImage(texture,(frame * 9) % 640,(frame * 5) % 360,1920,1080,0,0,1920,1080);
          context.fillStyle = '#11151fcc'; context.fillRect(75,90,1300,280);
        }
        context.fillStyle = '#b9a2ff'; context.font = 'bold 90px Segoe UI';
        context.fillText('SIDECAST / LIVE TEST', 110, 220);
        context.fillStyle = '#91d6a0'; context.font = '50px Segoe UI';
        context.fillText('1920 × 1080 · VIDEO + AUDIO', 110, 330);
        context.fillStyle = '#66588f'; context.fillRect(110, 460, 1700, 12);
        context.fillStyle = '#b9a2ff'; context.fillRect(110, 460, (frame * 9) % 1700, 12);
        context.fillStyle = '#92919f'; context.font = '40px Consolas';
        context.fillText(`FRAME ${frame}`, 110, 620);
      };
      draw();
      const fps = options.video.frameRate.max;
      const timer = setInterval(draw, 1000 / fps);
      const stream = canvas.captureStream(fps);
      let audioContext;
      if (options.audio && !(scenario === 'noaudio' && captureCalls === 1) && scenario !== 'noaudio-all') {
        audioContext = new AudioContext();
        const oscillator = audioContext.createOscillator(); oscillator.frequency.value = 220;
        const gain = audioContext.createGain(); gain.gain.value = .03;
        const destination = audioContext.createMediaStreamDestination();
        oscillator.connect(gain); gain.connect(destination); oscillator.start();
        stream.addTrack(destination.stream.getAudioTracks()[0]); await audioContext.resume();
      }
      const video = stream.getVideoTracks()[0];
      Object.defineProperty(video, 'label', { value: 'Тестовая игра · 1920×1080' });
      for (const track of stream.getTracks()) {
        const stop = track.stop.bind(track);
        track.stop = () => {
          stop();
          if (track.kind === 'video') clearInterval(timer);
          if (track.kind === 'audio') audioContext?.close().catch(() => {});
        };
      }
      return stream;
    },
  });
}
