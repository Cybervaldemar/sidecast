// Isolated browser QA. Synthetic capture is injected ONLY into this test browser.
import { chromium, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { once } from 'node:events';

const target = `${process.platform === 'win32' ? 'windows' : process.platform}-${process.arch === 'x64' ? 'amd64' : process.arch}`;
const binary = resolve(process.env.SIDECAST_BINARY || process.env.LANPLAY_BINARY || `build/${target}/sidecast-${target}${process.platform === 'win32' ? '.exe' : ''}`);
if (!existsSync(binary)) throw new Error('Build the standalone app first: npm run build:app');
const data = mkdtempSync(resolve(tmpdir(),'sidecast-e2e-'));
const origin = 'http://localhost:18080';
mkdirSync('test-results/v2',{recursive:true});
let server;
function startServer() {
  server = spawn(binary,['--no-tray','--listen','127.0.0.1:18080','--data',data],{windowsHide:true,stdio:['ignore','pipe','pipe']});
  server.stderr.on('data',chunk => process.stderr.write(chunk));
}
async function stopServer() {
  const current = server;
  server = undefined;
  if (current && current.exitCode === null && current.signalCode === null) {
    const done = once(current, 'exit');
    current.kill();
    await done;
  }
}
async function ready() {
  await expect.poll(async () => { try { return (await fetch(`${origin}/api/health`)).status; } catch { return 0; } },{timeout:15000}).toBe(200);
}
async function decodedVideo(page) {
  // Chrome can adapt resolution and suspend painting background/mobile tabs.
  // Receiver stats verify decoding independently of presentation and scaling.
  await expect.poll(() => page.evaluate(async () => {
    const peer = window.__testPeers.at(-1);
    if (peer?.connectionState !== 'connected') return false;
    for (const stat of (await peer.getStats()).values()) {
      if (stat.type === 'inbound-rtp' && stat.kind === 'video' && stat.framesDecoded > 1 && stat.frameWidth > 0 && stat.frameHeight > 0) return true;
    }
    return false;
  }), {timeout:25000}).toBe(true);
}
let browser;
try {
  startServer(); await ready();
  const chrome = process.env.SIDECAST_CHROME || process.env.LANPLAY_CHROME || (process.platform === 'win32' ? 'C:/Program Files/Google/Chrome/Application/chrome.exe' : '');
  browser = await chromium.launch({headless:true,...(chrome && existsSync(chrome) ? {executablePath:chrome} : {}), args:['--autoplay-policy=no-user-gesture-required','--disable-background-timer-throttling','--disable-renderer-backgrounding']});
  const studioContext = await browser.newContext({viewport:{width:1440,height:1100}});
  const errors = [];
  studioContext.on('page',page => page.on('pageerror',error => errors.push(error.message)));
  const captureScript = readFileSync(new URL('./fixtures/capture.js',import.meta.url),'utf8');
  await studioContext.addInitScript(captureScript);
  const studio = await studioContext.newPage(); await studio.goto(origin);
  await expect(studio).toHaveTitle('Sidecast — студия трансляций');
  await expect(studio.getByRole('link', { name: 'Sidecast — главная', exact: true })).toBeVisible();
  await expect(studio.getByRole('heading',{name:'Студия',exact:true})).toBeVisible();
  await studio.screenshot({path:'test-results/v2/studio-empty.png',fullPage:true});
  await studio.getByLabel('Никнейм',{exact:true}).fill('test-streamer');
  await studio.getByRole('button',{name:'Создать канал',exact:true}).click();
  await expect(studio.getByLabel('Постоянная ссылка')).toHaveValue(/\/watch\/test-streamer$/);
  const identity = await studio.evaluate(() => JSON.parse(localStorage.getItem('sidecast.identity.v2')));
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
  const volume = viewer.getByRole('slider', { name: 'Громкость', exact: true });
  await expect(volume).toHaveValue('100');
  await volume.press('Home');
  await expect.poll(() => viewer.locator('video').evaluate(video => video.volume)).toBe(0);
  await expect.poll(() => viewer.locator('video').evaluate(video => video.muted)).toBe(true);
  await volume.press('End');
  await volume.press('ArrowLeft');
  await expect.poll(() => viewer.locator('video').evaluate(video => video.volume)).toBe(0.99);
  await expect.poll(() => viewer.locator('video').evaluate(video => video.muted)).toBe(false);
  await viewer.getByRole('button', { name: 'Выключить звук', exact: true }).click();
  await expect(volume).toHaveValue('0');
  await viewer.getByRole('button', { name: 'Включить звук', exact: true }).click();
  await expect(volume).toHaveValue('99');
  await expect.poll(() => viewer.locator('video').evaluate(video => video.volume)).toBe(0.99);
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
  await decodedVideo(viewer);
  await studio.getByRole('button',{name:'Завершить эфир',exact:true}).click();
  await expect(viewer.getByRole('heading',{name:'test-streamer сейчас не в эфире'})).toBeVisible({timeout:12000});
  await stopServer(); startServer(); await ready();
  const status = await fetch(`${origin}/api/channels/test-streamer`).then(r=>r.json());
  expect(status.exists).toBe(true); expect(status.live).toBe(false);
  const reuse = await fetch(`${origin}/api/channels/test-streamer`,{method:'PUT',headers:{Authorization:`Bearer ${identity.token}`}});
  expect(reuse.status).toBe(200);
  const takeover = await fetch(`${origin}/api/channels/test-streamer`,{method:'PUT'}); expect(takeover.status).toBe(409);
  // Simulate an existing installation upgrading to the Sidecast name.
  await studio.evaluate(identity => {
    localStorage.setItem('lanplay.identity.v2', JSON.stringify(identity));
    localStorage.removeItem('sidecast.identity.v2');
  }, identity);
  await studio.reload(); await expect(studio.getByLabel('Постоянная ссылка')).toHaveValue(/\/watch\/test-streamer$/);
  await studio.getByRole('button',{name:'Начать трансляцию',exact:true}).click();
  await expect(studio.getByText('Вы в эфире',{exact:true})).toBeVisible({timeout:25000});
  await decodedVideo(viewer);
  await viewer.setViewportSize({width:390,height:844}); await viewer.screenshot({path:'test-results/v2/watch-mobile.png',fullPage:true});
  expect(await viewer.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await studio.setViewportSize({width:390,height:844}); await studio.screenshot({path:'test-results/v2/studio-mobile.png',fullPage:true});
  expect(await studio.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await studio.getByRole('button',{name:'Как это работает'}).click();
  await expect(studio.getByRole('heading',{name:'От экрана до зрителя.'})).toBeVisible();
  await studio.getByRole('button',{name:'Студия',exact:true}).click();
  await expect(studio.getByRole('button',{name:'Выключить приложение',exact:true})).toBeVisible();
  await studio.screenshot({path:'test-results/v2/studio-shutdown-mobile.png',fullPage:true});
  expect(await studio.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await expect(studio.getByText('Вы в эфире',{exact:true})).toBeVisible({timeout:25000});
  await decodedVideo(viewer);
  await expect(viewer.getByRole('button',{name:'Выключить приложение',exact:true})).toHaveCount(0);
  await studio.evaluate(() => { window.__shutdownTracks = [...document.querySelector('video').srcObject.getTracks()]; });
  const stopped = once(server, 'exit');
  await studio.getByRole('button',{name:'Выключить приложение',exact:true}).click();
  await expect(studio.getByRole('heading',{name:'Приложение выключено',exact:true})).toBeVisible();
  const [exitCode] = await Promise.race([stopped, new Promise((_, reject) => {
    const timer = setTimeout(() => reject(new Error('Application did not exit after shutdown')), 10000);
    timer.unref();
  })]);
  expect(exitCode).toBe(0);
  expect(await studio.evaluate(() => window.__shutdownTracks.every(track => track.readyState === 'ended'))).toBe(true);
  await expect.poll(async () => { try { await fetch(`${origin}/api/health`); return true; } catch { return false; } }).toBe(false);
  expect(errors).toEqual([]);
  const result = {passed:true,checks:['Sidecast page title and branding','waiting → decoded video + nonzero audio energy → offline → live','ownership persists across process restart','legacy LANPlay identity remains usable after renaming','same viewer page reconnects after server restart','nickname takeover denied','mobile layouts fit 390px','application button stops a live stream and exits the process with code 0','viewer has no shutdown button','no page errors'],screenshots:'test-results/v2',dataDirectory:data};
  writeFileSync('test-results/v2/results.json',JSON.stringify(result,null,2)); console.log(JSON.stringify(result,null,2));
} finally { await browser?.close(); await stopServer(); }
