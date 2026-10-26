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
const output = resolve(process.env.SIDECAST_TEST_OUTPUT || resolve(data, 'results'));
const origin = 'http://localhost:18080';
mkdirSync(output,{recursive:true});
let server;
let serverOutput = '';
function startServer(listen = ':18080') {
  serverOutput = '';
  server = spawn(binary,['--no-tray','--listen',listen,'--data',data],{windowsHide:true,stdio:['ignore','pipe','pipe']});
  server.stdout.on('data',chunk => { serverOutput += chunk; });
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
async function stereoAudio(page) {
  await page.evaluate(async () => {
    const context = new AudioContext({sampleRate:48000});
    const stream = new MediaStream(document.querySelector('video').srcObject.getAudioTracks());
    const source = context.createMediaStreamSource(stream);
    const splitter = context.createChannelSplitter(2);
    const silent = context.createGain();
    silent.gain.value = 0;
    silent.connect(context.destination);
    const channels = [context.createAnalyser(),context.createAnalyser()];
    source.connect(splitter);
    channels.forEach((analyser,index) => {
      analyser.fftSize = 8192;
      splitter.connect(analyser,index);
      analyser.connect(silent);
    });
    window.__testAudioMeter = {context,channels};
    await context.resume();
  });
  try {
    // The fixture sends 220 Hz on the left and 660 Hz on the right. Receiving
    // energy alone misses a mono downmix; verify the two channels stay distinct.
    await expect.poll(() => page.evaluate(() => {
      const {context,channels} = window.__testAudioMeter;
      const power = (analyser,frequency) => {
        const samples = new Float32Array(analyser.fftSize);
        analyser.getFloatTimeDomainData(samples);
        let real = 0;
        let imaginary = 0;
        for (let index = 0; index < samples.length; index++) {
          const phase = 2 * Math.PI * frequency * index / context.sampleRate;
          real += samples[index] * Math.cos(phase);
          imaginary += samples[index] * Math.sin(phase);
        }
        return (real * real + imaginary * imaginary) / (samples.length * samples.length);
      };
      const left = [power(channels[0],220),power(channels[0],660)];
      const right = [power(channels[1],220),power(channels[1],660)];
      return left[0] > 1e-7 && right[1] > 1e-7 && left[0] > left[1] * 30 && right[1] > right[0] * 30;
    }), {timeout:15000,message:'independent left/right game audio survives WebRTC forwarding'}).toBe(true);
  } finally {
    await page.evaluate(async () => {
      await window.__testAudioMeter.context.close();
      delete window.__testAudioMeter;
    });
  }
}
async function watchFits(page, viewport, screenshot) {
  await page.setViewportSize(viewport);
  await expect.poll(() => page.evaluate(() => {
    const root = document.documentElement;
    const bounds = selector => {
      const rect = document.querySelector(selector).getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && rect.left >= 0 && rect.top >= 0 && rect.right <= innerWidth + 1 && rect.bottom <= innerHeight + 1;
    };
    return root.scrollWidth <= innerWidth && root.scrollHeight <= innerHeight && bounds('.watch-player') && bounds('.watch-channel') && bounds('.watch-channel h1');
  })).toBe(true);
  await expect(page.getByRole('heading',{name:'test-streamer',exact:true})).toBeVisible();
  await expect(page.getByRole('contentinfo')).toHaveCount(0);
  await expect(page.getByRole('link',{name:'Открыть студию',exact:true})).toHaveCount(0);
  await expect(page.getByText('ПОСТОЯННЫЙ КАНАЛ',{exact:true})).toHaveCount(0);
  await page.screenshot({path:resolve(output,screenshot),fullPage:true});
}
async function viewerSurvivesControlGlitches(page) {
  const before = await page.evaluate(() => ({connections:window.__testPeers.length,stream:document.querySelector('video').srcObject?.id}));
  const unchanged = async () => {
    expect(await page.evaluate(() => ({connections:window.__testPeers.length,stream:document.querySelector('video').srcObject?.id}))).toEqual(before);
    expect(await page.evaluate(() => window.__testPeers.at(-1).connectionState)).toBe('connected');
  };
  // Losing one status response must not tear down an otherwise healthy RTP path.
  let failed = false;
  const failStatus = async route => {
    if (!failed) { failed = true; await route.abort('failed'); }
    else await route.continue();
  };
  await page.route('**/api/channels/test-streamer',failStatus);
  await expect.poll(() => failed,{timeout:5000}).toBe(true);
  await page.waitForTimeout(500);
  await unchanged();
  await page.unroute('**/api/channels/test-streamer',failStatus);
  // Dispatch the temporary disconnected/recovered transition without disrupting
  // this test machine's network. A stale 3 s timer used to close the recovered PC.
  await page.evaluate(() => {
    const peer = window.__testPeers.at(-1);
    Object.defineProperty(peer,'connectionState',{configurable:true,value:'disconnected'});
    peer.dispatchEvent(new Event('connectionstatechange'));
    delete peer.connectionState;
    peer.dispatchEvent(new Event('connectionstatechange'));
  });
  await page.waitForTimeout(3300);
  await unchanged();
  // Changes to LAN addresses are presentation metadata, not an ICE restart.
  let refreshed = false;
  const changeMetadata = async route => {
    const response = await route.fetch();
    const config = await response.json();
    await route.fulfill({response,json:{...config,addresses:[...config.addresses,'192.0.2.1']}});
    refreshed = true;
  };
  await page.route('**/api/config',changeMetadata);
  await expect.poll(() => refreshed,{timeout:18000}).toBe(true);
  await page.waitForTimeout(500);
  await unchanged();
  await page.unroute('**/api/config',changeMetadata);
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
  await studio.screenshot({path:resolve(output,'studio-empty.png'),fullPage:true});
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
  const viewer = await viewerContext.newPage();
  const viewerURL = new URL(await studio.getByLabel('Постоянная ссылка').inputValue());
  expect(viewerURL.protocol).toBe('http:');
  expect(['localhost', '127.0.0.1', '[::1]']).not.toContain(viewerURL.hostname);
  await viewer.goto(viewerURL.href);
  await expect(viewer.getByRole('heading',{name:'test-streamer сейчас не в эфире'})).toBeVisible();
  await watchFits(viewer,{width:1366,height:768},'waiting.png');
  await watchFits(viewer,{width:390,height:844},'waiting-mobile.png');
  await viewer.setViewportSize({width:1440,height:900});
  await studio.getByRole('button',{name:'Начать трансляцию',exact:true}).click();
  await expect(studio.getByText('Вы в эфире',{exact:true})).toBeVisible({timeout:25000});
  await expect.poll(() => viewer.locator('video').evaluate(video => video.videoWidth),{timeout:25000}).toBe(1920);
  await expect.poll(() => viewer.locator('video').evaluate(video => video.getVideoPlaybackQuality().totalVideoFrames),{timeout:10000}).toBeGreaterThan(10);
  await expect(viewer.locator('.watch-player').getByRole('status')).toHaveText(/в эфире/i);
  await expect(viewer.getByLabel(/^Зрителей:/)).toHaveText('1');
  await expect(viewer.locator('.watch-details')).toContainText(/\d+p/);
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
  await stereoAudio(viewer);
  await viewerSurvivesControlGlitches(viewer);
  const fullscreenButton = viewer.getByRole('button',{name:'Полный экран',exact:true});
  const exitFullscreenButton = viewer.getByRole('button',{name:'Выйти из полного экрана',exact:true});
  await fullscreenButton.click();
  await expect.poll(() => viewer.evaluate(() => document.fullscreenElement === document.querySelector('.watch-player .video-stage'))).toBe(true);
  await expect(exitFullscreenButton).toHaveAttribute('aria-pressed','true');
  await exitFullscreenButton.click();
  await expect.poll(() => viewer.evaluate(() => document.fullscreenElement === null)).toBe(true);
  await expect(fullscreenButton).toHaveAttribute('aria-pressed','false');
  await fullscreenButton.click();
  await expect(exitFullscreenButton).toBeVisible();
  await viewer.evaluate(() => document.exitFullscreen());
  await expect(fullscreenButton).toHaveAttribute('aria-pressed','false');
  await studio.screenshot({path:resolve(output,'studio-live.png'),fullPage:true});
  await viewer.screenshot({path:resolve(output,'watch-live.png'),fullPage:true});
  await fullscreenButton.click();
  await expect(exitFullscreenButton).toBeVisible();
  await studio.getByRole('button',{name:'Завершить эфир',exact:true}).click();
  await expect(viewer.getByRole('heading',{name:'test-streamer сейчас не в эфире'})).toBeVisible({timeout:12000});
  await exitFullscreenButton.click();
  await expect.poll(() => viewer.evaluate(() => document.fullscreenElement === null)).toBe(true);
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
  for (const [width,height,name] of [[1920,1080,'desktop'],[1366,768,'laptop'],[390,844,'mobile'],[844,390,'landscape'],[320,568,'small']]) {
    await watchFits(viewer,{width,height},`watch-${name}.png`);
    await expect(viewer.getByRole('slider',{name:'Громкость',exact:true})).toBeVisible();
    await expect(viewer.getByRole('button',{name:'Полный экран',exact:true})).toBeVisible();
    expect(await viewer.getByRole('button',{name:'Полный экран',exact:true}).evaluate(button => {
      const rect = button.getBoundingClientRect();
      return rect.left >= 0 && rect.top >= 0 && rect.right <= innerWidth && rect.bottom <= innerHeight;
    })).toBe(true);
  }
  await studio.setViewportSize({width:390,height:844}); await studio.screenshot({path:resolve(output,'studio-mobile.png'),fullPage:true});
  expect(await studio.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await studio.getByRole('button',{name:'Как это работает'}).click();
  await expect(studio.getByRole('heading',{name:'От экрана до зрителя.'})).toBeVisible();
  await studio.getByRole('button',{name:'Студия',exact:true}).click();
  await expect(studio.getByRole('button',{name:'Выключить приложение',exact:true})).toBeVisible();
  await studio.screenshot({path:resolve(output,'studio-shutdown-mobile.png'),fullPage:true});
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
  // Use the application's discovered LAN address, with the original HTTP transport.
  startServer(':0');
  await expect.poll(() => serverOutput, {timeout:15000}).toContain('Sidecast studio:');
  const localStudio = serverOutput.match(/Sidecast studio: (\S+)/)[1];
  const localURL = new URL(localStudio);
  expect(localURL.protocol).toBe('http:');
  expect(localURL.hostname).toBe('localhost');
  const networkConfig = await fetch(`${localURL.origin}/api/config`).then(response => response.json());
  expect(networkConfig.addresses.length).toBeGreaterThan(0);
  const networkURL = new URL(localStudio);
  const networkAddress = networkConfig.addresses[0];
  networkURL.hostname = networkAddress.includes(':') ? `[${networkAddress}]` : networkAddress;
  expect(['localhost', '127.0.0.1', '[::1]']).not.toContain(networkURL.hostname);
  const networkContext = await browser.newContext({viewport:{width:1440,height:1100}});
  const networkPage = await networkContext.newPage();
  networkPage.on('pageerror',error => errors.push(error.message));
  await networkPage.goto(networkURL.href);
  await expect(networkPage.getByRole('heading',{name:'Студия',exact:true})).toBeVisible();
  await networkPage.getByLabel('Никнейм',{exact:true}).fill('lan-streamer');
  await networkPage.getByRole('button',{name:'Создать канал',exact:true}).click();
  await expect(networkPage.getByLabel('Постоянная ссылка')).toHaveValue(`${networkURL.origin}/watch/lan-streamer`);
  const networkCapture = await networkPage.evaluate(() => ({
    secureContext: window.isSecureContext,
    captureAvailable: typeof navigator.mediaDevices?.getDisplayMedia === 'function',
  }));
  if (!networkCapture.captureAvailable) {
    await networkPage.getByRole('button',{name:'Начать трансляцию',exact:true}).click();
    await expect(networkPage.getByRole('alert')).toHaveText('Для захвата экрана откройте студию через localhost на компьютере ведущего.');
  }
  await expect(networkPage.getByRole('button',{name:'Выключить приложение',exact:true})).toHaveCount(0);
  await networkPage.screenshot({path:resolve(output,'studio-lan.png'),fullPage:true});
  const localPage = await networkContext.newPage();
  await localPage.goto(localStudio);
  expect(await localPage.evaluate(() => window.isSecureContext && typeof navigator.mediaDevices?.getDisplayMedia === 'function')).toBe(true);
  const networkStopped = once(server, 'exit');
  await localPage.getByRole('button',{name:'Выключить приложение',exact:true}).click();
  await expect(localPage.getByRole('heading',{name:'Приложение выключено',exact:true})).toBeVisible();
  const [networkExitCode] = await Promise.race([networkStopped, new Promise((_, reject) => {
    const timer = setTimeout(() => reject(new Error('LAN shutdown did not stop the application')), 10000);
    timer.unref();
  })]);
  expect(networkExitCode).toBe(0);
  expect(errors).toEqual([]);
  const result = {passed:true,checks:['Sidecast page title and branding','localhost publisher → LAN HTTP viewer with decoded video, nonzero audio energy and independent left/right stereo → offline → live','ownership persists across process restart','legacy LANPlay identity remains usable after renaming','same viewer page reconnects after server restart','status request failures, LAN metadata changes and recovered ICE transitions preserve the existing viewer connection','nickname takeover denied','viewer fits desktop, laptop, portrait and landscape phones down to 320px without scrolling','viewer has no footer or studio link; live badge stays inside the player','fullscreen button toggles both ways, follows browser exit and remains usable after the stream ends','application button stops a live stream and exits the process with code 0','viewer has no shutdown button','studio launch URL uses localhost and exposes the normal browser capture API','discovered LAN address opens the panel and generates share links over HTTP','shutdown is available through localhost only','no page errors'],networkCapture,screenshots:output,dataDirectory:data};
  writeFileSync(resolve(output, 'results.json'),JSON.stringify(result,null,2)); console.log(JSON.stringify(result,null,2));
} finally { await browser?.close(); await stopServer(); }
