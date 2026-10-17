// Isolated browser QA. Synthetic capture is injected ONLY into this test browser.
import { chromium, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { once } from 'node:events';

const binary = resolve(process.env.LANPLAY_BINARY || `build/lanplay-${process.platform === 'win32' ? 'windows' : process.platform}-${process.arch === 'x64' ? 'amd64' : process.arch}${process.platform === 'win32' ? '.exe' : ''}`);
if (!existsSync(binary)) throw new Error('Build the standalone app first: npm run build:app');
const data = mkdtempSync(resolve(tmpdir(),'lanplay-e2e-'));
const origin = 'http://localhost:18080';
mkdirSync('test-results/v2',{recursive:true});
let server;
function startServer() {
  server = spawn(binary,['--listen','127.0.0.1:18080','--data',data],{windowsHide:true,stdio:['ignore','pipe','pipe']});
  server.stderr.on('data',chunk => process.stderr.write(chunk));
}
async function stopServer() { if (server && server.exitCode === null) { const done = once(server,'exit'); server.kill(); await done; } }
async function ready() {
  await expect.poll(async () => { try { return (await fetch(`${origin}/api/health`)).status; } catch { return 0; } },{timeout:15000}).toBe(200);
}
let browser;
try {
  startServer(); await ready();
  const chrome = process.env.LANPLAY_CHROME || (process.platform === 'win32' ? 'C:/Program Files/Google/Chrome/Application/chrome.exe' : '');
  browser = await chromium.launch({headless:true,...(chrome && existsSync(chrome) ? {executablePath:chrome} : {}), args:['--autoplay-policy=no-user-gesture-required','--disable-background-timer-throttling','--disable-renderer-backgrounding']});
  const studioContext = await browser.newContext({viewport:{width:1440,height:1100}});
  const errors = [];
  studioContext.on('page',page => page.on('pageerror',error => errors.push(error.message)));
  const captureScript = readFileSync(new URL('./fixtures/capture.js',import.meta.url),'utf8');
  await studioContext.addInitScript(captureScript);
  const studio = await studioContext.newPage(); await studio.goto(origin);
  await expect(studio.getByRole('heading',{name:'Ваша студия.'})).toBeVisible();
  await studio.screenshot({path:'test-results/v2/studio-empty.png',fullPage:true});
  await studio.getByLabel('Никнейм',{exact:true}).fill('test-streamer');
  await studio.getByRole('button',{name:'Создать канал',exact:true}).click();
  await expect(studio.getByLabel('Постоянная ссылка')).toHaveValue(/\/watch\/test-streamer$/);
  const identity = await studio.evaluate(() => JSON.parse(localStorage.getItem('lanplay.identity.v2')));
  const viewerContext = await browser.newContext({viewport:{width:1440,height:1100}});
  await viewerContext.addInitScript(() => {
    window.__testPeers = [];
    const Original = window.RTCPeerConnection;
    window.RTCPeerConnection = class extends Original { constructor(...args) { super(...args); window.__testPeers.push(this); } };
  });
  viewerContext.on('page',page => page.on('pageerror',error => errors.push(error.message)));
  const viewer = await viewerContext.newPage(); await viewer.goto(`${origin}/watch/test-streamer`);
  await expect(viewer.getByRole('heading',{name:'test-streamer сейчас не в эфире'})).toBeVisible();
  await viewer.screenshot({path:'test-results/v2/waiting.png',fullPage:true});
  await studio.getByRole('button',{name:'Начать трансляцию',exact:true}).click();
  await expect(studio.getByText('Вы в эфире',{exact:true})).toBeVisible({timeout:25000});
  await expect.poll(() => viewer.locator('video').evaluate(video => video.videoWidth),{timeout:25000}).toBe(1920);
  await expect.poll(() => viewer.locator('video').evaluate(video => video.getVideoPlaybackQuality().totalVideoFrames),{timeout:10000}).toBeGreaterThan(10);
  await viewer.getByRole('button',{name:'Включить звук',exact:true}).click();
  await expect.poll(() => viewer.locator('video').evaluate(video => video.srcObject?.getAudioTracks().length)).toBe(1);
  await expect.poll(() => viewer.locator('video').evaluate(video => video.muted)).toBe(false);
  await expect.poll(() => viewer.evaluate(async () => {
    for (const peer of window.__testPeers) for (const stat of (await peer.getStats()).values()) {
      if (stat.type === 'inbound-rtp' && stat.kind === 'audio' && stat.bytesReceived > 0 && stat.totalAudioEnergy > 0) return true;
    }
    return false;
  }),{timeout:15000}).toBe(true);
  await studio.screenshot({path:'test-results/v2/studio-live.png',fullPage:true});
  await viewer.screenshot({path:'test-results/v2/watch-live.png',fullPage:true});
  await studio.getByRole('button',{name:'Завершить эфир',exact:true}).click();
  await expect(viewer.getByRole('heading',{name:'test-streamer сейчас не в эфире'})).toBeVisible({timeout:12000});
  await studio.getByRole('button',{name:'Начать трансляцию',exact:true}).click();
  await expect.poll(() => viewer.locator('video').evaluate(video => video.videoWidth),{timeout:25000}).toBe(1920);
  await studio.getByRole('button',{name:'Завершить эфир',exact:true}).click();
  await stopServer(); startServer(); await ready();
  const status = await fetch(`${origin}/api/channels/test-streamer`).then(r=>r.json());
  expect(status.exists).toBe(true); expect(status.live).toBe(false);
  const reuse = await fetch(`${origin}/api/channels/test-streamer`,{method:'PUT',headers:{Authorization:`Bearer ${identity.token}`}});
  expect(reuse.status).toBe(200);
  const takeover = await fetch(`${origin}/api/channels/test-streamer`,{method:'PUT'}); expect(takeover.status).toBe(409);
  await studio.reload(); await expect(studio.getByLabel('Постоянная ссылка')).toHaveValue(/\/watch\/test-streamer$/);
  await studio.getByRole('button',{name:'Начать трансляцию',exact:true}).click();
  await expect.poll(() => viewer.locator('video').evaluate(video => video.videoWidth),{timeout:25000}).toBe(1920);
  await studio.getByRole('button',{name:'Завершить эфир',exact:true}).click();
  await viewer.setViewportSize({width:390,height:844}); await viewer.screenshot({path:'test-results/v2/watch-mobile.png',fullPage:true});
  expect(await viewer.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await studio.setViewportSize({width:390,height:844}); await studio.screenshot({path:'test-results/v2/studio-mobile.png',fullPage:true});
  expect(await studio.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await studio.getByRole('button',{name:'Как это работает'}).click();
  await expect(studio.getByRole('heading',{name:'От экрана до зрителя.'})).toBeVisible();
  expect(errors).toEqual([]);
  const result = {passed:true,checks:['waiting → decoded video + nonzero audio energy → offline → live','ownership persists across process restart','same viewer page reconnects after server restart','nickname takeover denied','mobile layouts fit 390px','no page errors'],screenshots:'test-results/v2',dataDirectory:data};
  writeFileSync('test-results/v2/results.json',JSON.stringify(result,null,2)); console.log(JSON.stringify(result,null,2));
} finally { await browser?.close(); await stopServer(); }
