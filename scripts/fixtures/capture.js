if (navigator.mediaDevices && !location.pathname.startsWith('/watch/')) {
  let captureCalls = 0;
  Object.defineProperty(navigator.mediaDevices, 'getDisplayMedia', {
    configurable: true,
    value: async options => {
      captureCalls++;
      (window.__testCaptureOptions ??= []).push(structuredClone(options));
      const query = new URL(location.href).searchParams;
      const scenario = query.get('capture');
      if (scenario === 'cancel') throw new DOMException('Test cancellation', 'NotAllowedError');
      const width = Number(query.get('captureWidth') || 1920);
      const height = Number(query.get('captureHeight') || 1080);
      if (!Number.isInteger(width) || !Number.isInteger(height) || width < 320 || height < 180 || width > 3840 || height > 2160)
        throw new Error('Invalid synthetic capture dimensions');
      const canvas = document.createElement('canvas'); canvas.width = width; canvas.height = height;
      const context = canvas.getContext('2d');
      let texture;
      if (scenario === 'motion') {
        texture = document.createElement('canvas'); texture.width = width + 640; texture.height = height + 360;
        const pixels = texture.getContext('2d');
        let seed = 12345;
        for (let y = 0; y < texture.height; y += 16) for (let x = 0; x < texture.width; x += 16) {
          seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
          pixels.fillStyle = `rgb(${seed & 255},${(seed >>> 8) & 255},${(seed >>> 16) & 255})`;
          pixels.fillRect(x, y, 16, 16);
        }
      }
      const fps = options.video.frameRate.max;
      window.__testCapture = { width, height, fps, startedAt: performance.now(), framesDrawn: 0, lastDrawAt: 0 };
      let frame = 0;
      const draw = () => {
        frame++;
        window.__testCapture.framesDrawn = frame;
        window.__testCapture.lastDrawAt = performance.now();
        context.setTransform(1, 0, 0, 1, 0, 0);
        context.fillStyle = '#11151f'; context.fillRect(0, 0, width, height);
        if (texture) {
          context.drawImage(texture,(frame * 9) % 640,(frame * 5) % 360,width,height,0,0,width,height);
        }
        context.scale(width / 1920, height / 1080);
        if (texture) {
          context.fillStyle = '#11151fcc'; context.fillRect(75,90,1300,280);
        }
        context.fillStyle = '#b9a2ff'; context.font = 'bold 90px Segoe UI';
        context.fillText('SIDECAST / LIVE TEST', 110, 220);
        context.fillStyle = '#91d6a0'; context.font = '50px Segoe UI';
        context.fillText(`${width} × ${height} · VIDEO + AUDIO`, 110, 330);
        context.fillStyle = '#66588f'; context.fillRect(110, 460, 1700, 12);
        context.fillStyle = '#b9a2ff'; context.fillRect(110, 460, (frame * 9) % 1700, 12);
        context.fillStyle = '#92919f'; context.font = '40px Consolas';
        context.fillText(`FRAME ${frame}`, 110, 620);
      };
      draw();
      const timer = setInterval(draw, 1000 / fps);
      const stream = canvas.captureStream(fps);
      let audioContext;
      if (options.audio && !(scenario === 'noaudio' && captureCalls === 1) && scenario !== 'noaudio-all') {
        audioContext = new AudioContext({ sampleRate: 48000 });
        const stereo = audioContext.createChannelMerger(2);
        for (const [channel, frequency] of [220, 660].entries()) {
          const oscillator = audioContext.createOscillator(); oscillator.frequency.value = frequency;
          const gain = audioContext.createGain(); gain.gain.value = .03;
          oscillator.connect(gain); gain.connect(stereo, 0, channel); oscillator.start();
        }
        const destination = audioContext.createMediaStreamDestination();
        stereo.connect(destination);
        stream.addTrack(destination.stream.getAudioTracks()[0]); await audioContext.resume();
      }
      const video = stream.getVideoTracks()[0];
      Object.defineProperty(video, 'label', { value: `Тестовая игра · ${width}×${height}` });
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
