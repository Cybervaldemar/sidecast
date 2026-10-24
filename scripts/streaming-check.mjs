// Isolated 1080p60 motion test. Packet loss applies only to this test browser.
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
const bitrates = (process.env.SIDECAST_TEST_BITRATES || '12,20,40').split(',').map(Number);
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

async function snapshot(page, outbound) {
  return page.evaluate(async (outbound) => {
    const peer = window.__testPeers.at(-1);
    if (!peer) return {};
    const reports = await peer.getStats();
    const report = [...reports.values()].find(
      (value) =>
        value.type === (outbound ? 'outbound-rtp' : 'inbound-rtp') && value.kind === 'video',
    );
    if (!report) return { connections: window.__testPeers.length, state: peer.connectionState };
    const keys = [
      'bytesSent',
      'bytesReceived',
      'packetsReceived',
      'packetsLost',
      'framesEncoded',
      'framesDecoded',
      'framesDropped',
      'framesPerSecond',
      'freezeCount',
      'totalFreezesDuration',
      'totalEncodeTime',
      'keyFramesDecoded',
      'nackCount',
      'pliCount',
      'retransmittedPacketsSent',
      'qualityLimitationReason',
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
  await studio.goto(`${origin}/studio?capture=motion`);
  await studio.getByLabel('Никнейм', { exact: true }).fill('motion-check');
  await studio.getByRole('button', { name: 'Создать канал', exact: true }).click();
  const viewer = await viewerContext.newPage();
  await viewer.goto(await studio.getByLabel('Постоянная ссылка').inputValue());
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
    `Streaming artifacts: ${directory}; Chrome ${browser.version()}; simulated packet loss ${loss}%`,
  );
  for (const bitrate of bitrates) {
    await studio.locator('#bitrate').selectOption(String(bitrate));
    await studio.getByRole('button', { name: 'Начать трансляцию', exact: true }).click();
    await expect(studio.getByText('Вы в эфире', { exact: true })).toBeVisible({ timeout: 25000 });
    await expect
      .poll(async () => (await snapshot(viewer, false)).framesDecoded || 0, { timeout: 25000 })
      .toBeGreaterThan(60);
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
    const result = {
      capMbps: bitrate,
      durationSeconds: duration,
      codec: last.receiver.codec,
      sentMbps: rate('publisher', 'bytesSent', 8e-6),
      receivedMbps: rate('receiver', 'bytesReceived', 8e-6),
      encodedFps: rate('publisher', 'framesEncoded'),
      decodedFps: rate('receiver', 'framesDecoded'),
      freezeCount: difference('receiver', 'freezeCount'),
      freezeSeconds: difference('receiver', 'totalFreezesDuration'),
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
      peersBefore: first.receiver.connections,
      peersAfter: last.receiver.connections,
      samples,
    };
    results.push(result);
    writeFileSync(
      resolve(directory, 'results.json'),
      JSON.stringify({ seconds, loss, binary, results, serverErrors }, null, 2),
    );
    console.log(JSON.stringify({ ...result, samples: undefined }));
    await studio.getByRole('button', { name: 'Завершить эфир', exact: true }).click();
    await expect(
      viewer.getByRole('heading', { name: 'motion-check сейчас не в эфире' }),
    ).toBeVisible({ timeout: 10000 });
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
