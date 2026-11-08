// Isolated native-resolution motion test. Packet loss applies only to this test browser.
import { chromium, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { once } from 'node:events';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';

const platform = `${process.platform === 'win32' ? 'windows' : process.platform}-${process.arch === 'x64' ? 'amd64' : process.arch}`;
const binary = resolve(
  process.env.SIDECAST_BINARY ||
    `build/${platform}/sidecast-${platform}${process.platform === 'win32' ? '.exe' : ''}`,
);
const directory = mkdtempSync(resolve(tmpdir(), 'sidecast-streaming-'));
const seconds = Number(process.env.SIDECAST_TEST_SECONDS || 25);
const loss = Number(process.env.SIDECAST_PACKET_LOSS || 0.5);
const bitrates = (process.env.SIDECAST_TEST_BITRATES || '6,12,20').split(',').map(Number);
const width = Number(process.env.SIDECAST_TEST_WIDTH || 2560);
const height = Number(process.env.SIDECAST_TEST_HEIGHT || 1440);
const videoPriority = process.env.SIDECAST_VIDEO_PRIORITY || 'resolution';
if (!Number.isFinite(seconds) || seconds < 3)
  throw new Error('SIDECAST_TEST_SECONDS must be at least 3');
if (!['resolution', 'motion'].includes(videoPriority))
  throw new Error('Invalid SIDECAST_VIDEO_PRIORITY');
if (!existsSync(binary)) throw new Error('Build Sidecast before running the streaming check');
const server = spawn(binary, ['--no-tray', '--listen', ':0', '--data', directory], {
  windowsHide: true,
  stdio: ['ignore', 'pipe', 'pipe'],
});
let output = '';
server.stdout.on('data', (chunk) => {
  output += chunk;
});
let serverErrors = '';
server.stderr.on('data', (chunk) => {
  serverErrors += chunk;
});
let browser;
const results = [];

function writeReport() {
  writeFileSync(
    resolve(directory, 'results.json'),
    JSON.stringify(
      {
        seconds,
        loss,
        binary,
        width,
        height,
        videoPriority,
        results,
        serverErrors,
        serverOutput: output,
      },
      null,
      2,
    ),
  );
}

async function snapshot(page, outbound) {
  return page.evaluate(async (outbound) => {
    const peer = window.__testPeers.at(-1);
    if (!peer) return {};
    const reports = await peer.getStats();
    const report = [...reports.values()].find(
      (value) =>
        value.type === (outbound ? 'outbound-rtp' : 'inbound-rtp') && value.kind === 'video',
    );
    const audio = [...reports.values()].find(
      (value) =>
        value.type === (outbound ? 'outbound-rtp' : 'inbound-rtp') && value.kind === 'audio',
    );
    const videoSender = peer.getSenders().find((sender) => sender.track?.kind === 'video');
    const audioSender = peer.getSenders().find((sender) => sender.track?.kind === 'audio');
    if (!report) return { connections: window.__testPeers.length, state: peer.connectionState };
    const keys = [
      'bytesSent',
      'bytesReceived',
      'packetsReceived',
      'packetsLost',
      'framesEncoded',
      'framesSent',
      'framesDecoded',
      'framesDropped',
      'framesPerSecond',
      'freezeCount',
      'totalFreezesDuration',
      'totalEncodeTime',
      'qpSum',
      'targetBitrate',
      'totalPacketSendDelay',
      'keyFramesDecoded',
      'nackCount',
      'pliCount',
      'retransmittedPacketsSent',
      'qualityLimitationReason',
      'qualityLimitationResolutionChanges',
      'qualityLimitationDurations',
      'jitter',
      'jitterBufferDelay',
      'jitterBufferEmittedCount',
      'frameWidth',
      'frameHeight',
    ];
    return {
      ...Object.fromEntries(keys.map((key) => [key, report[key]])),
      id: report.id,
      codec: reports.get(report.codecId)?.mimeType,
      connections: window.__testPeers.length,
      state: peer.connectionState,
      sender: videoSender?.getParameters(),
      source: videoSender?.track.getSettings(),
      sourceStats: reports.get(report.mediaSourceId),
      fixture: window.__testCapture && {
        ...window.__testCapture,
        now: performance.now(),
        hidden: document.hidden,
      },
      bandwidth: [...reports.values()].find(
        (value) =>
          value.type === 'candidate-pair' && value.nominated && value.state === 'succeeded',
      )?.availableOutgoingBitrate,
      audio: audio && {
        bytes: outbound ? audio.bytesSent : audio.bytesReceived,
        packetsLost: audio.packetsLost,
        jitter: audio.jitter,
        concealedSamples: audio.concealedSamples,
        totalSamplesReceived: audio.totalSamplesReceived,
        totalAudioEnergy: audio.totalAudioEnergy,
        codec: reports.get(audio.codecId),
        sender: audioSender?.getParameters(),
        source: audioSender?.track.getSettings(),
      },
    };
  }, outbound);
}

try {
  await expect.poll(() => output, { timeout: 15000 }).toContain('Sidecast studio:');
  const origin = new URL(output.match(/Sidecast studio: (\S+)/)[1]).origin;
  const chrome =
    process.env.SIDECAST_CHROME ||
    (process.platform === 'win32' ? 'C:/Program Files/Google/Chrome/Application/chrome.exe' : '');
  browser = await chromium.launch({
    headless: true,
    ...(chrome && existsSync(chrome) ? { executablePath: chrome } : {}),
    args: [
      '--autoplay-policy=no-user-gesture-required',
      '--disable-background-timer-throttling',
      '--disable-renderer-backgrounding',
    ],
  });
  const source = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const viewerContext = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  await source.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: async (link) => { window.__testCopiedLink = link; } },
    });
  });
  for (const context of [source, viewerContext])
    await context.addInitScript(() => {
      window.__testPeers = [];
      const Original = window.RTCPeerConnection;
      window.RTCPeerConnection = class extends Original {
        constructor(...args) {
          super(...args);
          window.__testPeers.push(this);
        }
      };
    });
  await source.addInitScript(
    readFileSync(new URL('./fixtures/capture.js', import.meta.url), 'utf8'),
  );
  const studio = await source.newPage();
  await studio.goto(
    `${origin}/studio?capture=motion&captureWidth=${width}&captureHeight=${height}`,
  );
  await studio.getByLabel('Никнейм', { exact: true }).fill('motion-check');
  await studio.getByRole('button', { name: 'Создать канал', exact: true }).click();
  await studio.getByRole('button', { name: 'Ссылка на стрим', exact: true }).click();
  const viewerURL = await studio.evaluate(() => window.__testCopiedLink);
  if (new URL(viewerURL).pathname !== '/watch/motion-check')
    throw new Error('Copied stream link is invalid');
  const viewer = await viewerContext.newPage();
  await viewer.goto(viewerURL);
  const cdp = await viewerContext.newCDPSession(viewer);
  await cdp.send('Network.emulateNetworkConditionsByRule', {
    matchedNetworkConditions: [
      {
        urlPattern: '',
        latency: 20,
        downloadThroughput: -1,
        uploadThroughput: -1,
        packetLoss: loss,
        packetQueueLength: 0,
        packetReordering: false,
      },
    ],
  });
  console.log(
    `Streaming artifacts: ${directory}; Chrome ${browser.version()}; ${width}x${height}, ${videoPriority} priority; simulated packet loss ${loss}%`,
  );
  await studio.locator('#video-priority').selectOption(videoPriority);
  for (const bitrate of bitrates) {
    await studio.getByRole('button', { name: 'Начать трансляцию', exact: true }).click();
    const warmupSamples = [];
    const warmupStarted = Date.now();
    let warmupStage = 'video';
    try {
      await expect(studio.getByText('Вы в эфире', { exact: true })).toBeVisible({ timeout: 25000 });
      // Diagnostic runs can compare encoder caps without exposing a quality
      // selector in the studio. The product itself uses one fixed cap.
      await studio.evaluate(async (limit) => {
        const peer = window.__testPeers.at(-1);
        const sender = peer?.getSenders().find((value) => value.track?.kind === 'video');
        if (!sender) throw new Error('Video sender is unavailable');
        const parameters = sender.getParameters();
        if (!parameters.encodings.length) throw new Error('Video encoding is unavailable');
        for (const encoding of parameters.encodings) encoding.maxBitrate = limit * 1_000_000;
        await sender.setParameters(parameters);
      }, bitrate);
      await expect
        .poll(
          async () => {
            const [publisher, receiver] = await Promise.all([
              snapshot(studio, true),
              snapshot(viewer, false),
            ]);
            warmupSamples.push({ elapsedMs: Date.now() - warmupStarted, publisher, receiver });

            return receiver.framesDecoded || 0;
          },
          { timeout: 25000, intervals: [1000] },
        )
        .toBeGreaterThan(60);
      warmupStage = 'audio';
      if (await viewer.locator('video').evaluate((video) => video.muted))
        await viewer.getByRole('button', { name: 'Включить звук', exact: true }).click();
      await expect.poll(() => viewer.locator('video').evaluate((video) => video.muted)).toBe(false);
      await expect
        .poll(async () => (await snapshot(viewer, false)).audio?.totalAudioEnergy || 0, {
          timeout: 10000,
        })
        .toBeGreaterThan(0);
    } catch (error) {
      const latest = await Promise.allSettled([snapshot(studio, true), snapshot(viewer, false)]);
      const finalSnapshot = Object.fromEntries(
        ['publisher', 'receiver'].map((side, index) => [
          side,
          latest[index].status === 'fulfilled'
            ? latest[index].value
            : { error: String(latest[index].reason) },
        ]),
      );
      results.push({
        passed: false,
        stage: `${warmupStage}-warmup`,
        capMbps: bitrate,
        failures: [String(error)],
        durationSeconds: (Date.now() - warmupStarted) / 1000,
        sampleCount: warmupSamples.length,
        ...finalSnapshot,
        samples: warmupSamples,
      });
      writeReport();
      console.error(`Streaming warm-up failed; diagnostics: ${resolve(directory, 'results.json')}`);

      throw error;
    }
    await viewer.bringToFront();
    await viewer.evaluate(() => {
      window.__presentation = { last: 0, maxGap: 0, frames: 0 };
      const video = document.querySelector('video');
      const next = (now) => {
        const value = window.__presentation;
        if (value.last) value.maxGap = Math.max(value.maxGap, now - value.last);
        value.last = now;
        value.frames++;
        value.callback = video.requestVideoFrameCallback(next);
      };
      window.__presentation.callback = video.requestVideoFrameCallback(next);
    });
    const samples = [];
    const started = Date.now();
    while (Date.now() - started < seconds * 1000) {
      const [publisher, receiver] = await Promise.all([
        snapshot(studio, true),
        snapshot(viewer, false),
      ]);
      samples.push({ elapsedMs: Date.now() - started, publisher, receiver });
      if (samples.length % 5 === 0)
        console.log(
          `${bitrate} Mbit/s cap, ${Math.round((Date.now() - started) / 1000)} s: ${receiver.framesPerSecond ?? 0} FPS, freezes ${receiver.freezeCount ?? 'unavailable'}, peers ${receiver.connections}`,
        );
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
    const presentation = await viewer.evaluate(() => {
      const value = window.__presentation;
      document.querySelector('video').cancelVideoFrameCallback(value.callback);
      return value;
    });
    const first = samples[0];
    const last = samples.at(-1);
    const duration = (last.elapsedMs - first.elapsedMs) / 1000;
    const difference = (side, key) => {
      let total = 0;
      let available = false;
      for (let index = 1; index < samples.length; index++) {
        const previous = samples[index - 1][side];
        const current = samples[index][side];
        if (previous[key] == null || current[key] == null) continue;
        available = true;
        total +=
          current.connections === previous.connections && current.id === previous.id
            ? current[key] - previous[key]
            : current[key];
      }

      return available ? total : null;
    };
    const rate = (side, key, scale = 1) => {
      const value = difference(side, key);

      return value == null ? null : (value * scale) / duration;
    };
    const encoded = difference('publisher', 'framesEncoded');
    const failures = [];
    let decodeStallMs = 0;
    let maxDecodeStallMs = 0;
    for (let index = 1; index < samples.length; index++) {
      const previous = samples[index - 1];
      const current = samples[index];
      if (
        current.receiver.connections !== previous.receiver.connections ||
        current.receiver.id !== previous.receiver.id ||
        (current.receiver.framesDecoded ?? 0) > (previous.receiver.framesDecoded ?? 0)
      ) {
        decodeStallMs = 0;
      } else {
        decodeStallMs += current.elapsedMs - previous.elapsedMs;
        maxDecodeStallMs = Math.max(maxDecodeStallMs, decodeStallMs);
      }
    }
    if (maxDecodeStallMs >= 5000)
      failures.push(`Viewer decoded no new frames for ${Math.round(maxDecodeStallMs)} ms`);
    if (last.receiver.connections !== first.receiver.connections)
      failures.push('Viewer reconnected during playback');
    if (videoPriority === 'resolution') {
      for (const side of ['publisher', 'receiver']) {
        const dimensions = samples.map((sample) => [
          sample[side].frameWidth,
          sample[side].frameHeight,
        ]);
        if (
          dimensions.some(
            ([sampleWidth, sampleHeight]) => sampleWidth !== width || sampleHeight !== height,
          )
        )
          failures.push(
            `${side} changed resolution: ${JSON.stringify([...new Set(dimensions.map((size) => size.join('x')))])}`,
          );
      }
    }
    if (last.receiver.audio?.totalAudioEnergy == null || last.receiver.audio.totalAudioEnergy <= 0)
      failures.push('Viewer did not decode audible audio');
    const result = {
      passed: failures.length === 0,
      failures,
      width,
      height,
      videoPriority,
      capMbps: bitrate,
      durationSeconds: duration,
      codec: last.receiver.codec,
      sentMbps: rate('publisher', 'bytesSent', 8e-6),
      receivedMbps: rate('receiver', 'bytesReceived', 8e-6),
      encodedFps: rate('publisher', 'framesEncoded'),
      decodedFps: rate('receiver', 'framesDecoded'),
      freezeCount: difference('receiver', 'freezeCount'),
      freezeSeconds: difference('receiver', 'totalFreezesDuration'),
      maxDecodeStallMs,
      maxPresentationGapMs: presentation.maxGap,
      packetsLost: difference('receiver', 'packetsLost'),
      nackCount: difference('receiver', 'nackCount'),
      pliCount: difference('receiver', 'pliCount'),
      encodeMsPerFrame: encoded
        ? (difference('publisher', 'totalEncodeTime') * 1000) / encoded
        : null,
      qualityLimitations: [
        ...new Set(samples.map((sample) => sample.publisher.qualityLimitationReason)),
      ],
      resolutions: Object.fromEntries(
        ['publisher', 'receiver'].map((side) => [
          side,
          [
            ...new Set(
              samples.map((sample) => `${sample[side].frameWidth}x${sample[side].frameHeight}`),
            ),
          ],
        ]),
      ),
      audio: last.receiver.audio,
      peersBefore: first.receiver.connections,
      peersAfter: last.receiver.connections,
      samples,
    };
    results.push(result);
    writeReport();
    console.log(JSON.stringify({ ...result, samples: undefined }));
    await studio.getByRole('button', { name: 'Завершить эфир', exact: true }).click();
    await expect(
      viewer.getByRole('heading', { name: 'motion-check сейчас не в эфире' }),
    ).toBeVisible({ timeout: 10000 });
    expect(failures, `Streaming regression at ${bitrate} Mbit/s; details: ${directory}`).toEqual(
      [],
    );
  }
  console.log(`Results: ${resolve(directory, 'results.json')}`);
} finally {
  await browser?.close();
  if (server.exitCode === null && server.signalCode === null) {
    const stopped = once(server, 'exit');
    server.kill();
    await stopped;
  }
}
