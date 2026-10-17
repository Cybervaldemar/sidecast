// Windows-only opt-in test. Captures the dedicated fixture window and its audio.
import { chromium, expect } from '@playwright/test';
import { spawn, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { once } from 'node:events';

if (process.platform !== 'win32' || !process.env.LANPLAY_NATIVE_FFMPEG) throw new Error('Windows and LANPLAY_NATIVE_FFMPEG required. This test opens its own animated window and plays a quiet test tone.');
const ffmpeg=resolve(process.env.LANPLAY_NATIVE_FFMPEG);
const serverBinary=resolve(process.env.LANPLAY_BINARY || 'build/optimized/lanplay-windows-amd64.exe');
const studioBinary=resolve('build/studio/windows-amd64/lanplay-studio.exe');
const directory=resolve('test-results/native'); mkdirSync(directory,{recursive:true});
const fixture=resolve(directory,'native-source.exe');
const compiler=resolve(process.env.WINDIR || 'C:/Windows','Microsoft.NET/Framework64/v4.0.30319/csc.exe');
const compilation=spawnSync(compiler,['/nologo','/target:winexe','/reference:System.Windows.Forms.dll','/reference:System.Drawing.dll',`/out:${fixture}`,resolve('scripts/fixtures/NativeSource.cs')],{stdio:'inherit',windowsHide:true});
if(compilation.status!==0) throw new Error('Fixture compilation failed');
const children=[]; let browser;
function child(executable,args=[]) { const p=spawn(executable,args,{windowsHide:true,stdio:['ignore','pipe','pipe']}); p.stderr.on('data',data=>process.stderr.write(data)); children.push(p); return p; }
const origin='http://127.0.0.1:18765', upstream='http://127.0.0.1:18080';
try {
  child(serverBinary,['--listen','127.0.0.1:18080','--data',mkdtempSync(resolve(tmpdir(),'lanplay-native-'))]);
  await expect.poll(async()=>{try{return(await fetch(upstream+'/api/health')).status;}catch{return 0;}},{timeout:15000}).toBe(200);
  child(studioBinary,['--port','18765','--server',upstream,'--ffmpeg',ffmpeg]);
  await expect.poll(async()=>{try{return(await fetch(origin+'/api/config')).status;}catch{return 0;}},{timeout:15000}).toBe(200);
  const source=child(fixture);
  let sourceID;
  await expect.poll(async()=> { const list=await fetch(origin+'/api/native/sources').then(r=>r.json()); sourceID=list.find(s=>s.processId===source.pid)?.id; return !!sourceID; },{timeout:10000}).toBe(true);
  const chrome=process.env.LANPLAY_CHROME || 'C:/Program Files/Google/Chrome/Application/chrome.exe';
  browser=await chromium.launch({headless:true,...(existsSync(chrome)?{executablePath:chrome}:{}),args:['--autoplay-policy=no-user-gesture-required','--disable-background-timer-throttling']});
  const context=await browser.newContext({viewport:{width:1440,height:1100}}); const errors=[];
  context.on('page',page=>page.on('pageerror',error=>errors.push(error.message)));
  const page=await context.newPage(); await page.goto(origin);
  await page.getByLabel('Никнейм',{exact:true}).fill('native-test'); await page.getByRole('button',{name:'Создать канал',exact:true}).click();
  await page.getByLabel('Режим',{exact:true}).selectOption('native');
  await expect(page.locator('#native-source option').filter({hasText:'LANPlay native integration source'})).toHaveCount(1);
  await page.getByLabel('Окно или экран',{exact:true}).selectOption(sourceID);
  await expect(page.getByLabel('Приложение для звука',{exact:true})).toHaveValue(sourceID);
  await page.getByLabel('Кодировщик',{exact:true}).selectOption('libx264');
  const viewer=await context.newPage();
  await viewer.addInitScript(()=>{ window.__testPeers=[]; const Original=window.RTCPeerConnection; window.RTCPeerConnection=class extends Original { constructor(...args){ super(...args); window.__testPeers.push(this); } }; });
  await viewer.goto(upstream+'/watch/native-test');
  await page.getByRole('button',{name:'Начать трансляцию',exact:true}).click();
  await expect(page.getByText('Вы в эфире',{exact:true})).toBeVisible({timeout:60000});
  await expect.poll(()=>viewer.locator('video').evaluate(video=>video.videoWidth),{timeout:20000}).toBeGreaterThanOrEqual(640);
  await expect.poll(()=>viewer.locator('video').evaluate(video=>video.getVideoPlaybackQuality().totalVideoFrames),{timeout:15000}).toBeGreaterThan(20);
  await viewer.getByRole('button',{name:'Включить звук',exact:true}).click();
  await expect.poll(()=>viewer.evaluate(async()=>{
    for(const pc of window.__testPeers) for(const stat of(await pc.getStats()).values()) if(stat.type==='inbound-rtp' && stat.kind==='audio' && stat.totalAudioEnergy>0) return true;
    return false;
  }),{timeout:15000}).toBe(true);
  const status=await page.evaluate(async()=>{const identity=JSON.parse(localStorage.getItem('lanplay.identity.v2'));return fetch('/api/native/status',{headers:{Authorization:'Bearer '+identity.token}}).then(r=>r.json());});
  await page.screenshot({path:resolve(directory,'studio.png'),fullPage:true}); await viewer.screenshot({path:resolve(directory,'viewer.png'),fullPage:true});
  await page.getByRole('button',{name:'Завершить эфир',exact:true}).click();
  await expect(viewer.getByRole('heading',{name:'native-test сейчас не в эфире'})).toBeVisible({timeout:12000});
  await page.getByLabel('Источник звука',{exact:true}).selectOption('system');
  await page.getByRole('button',{name:'Начать трансляцию',exact:true}).click();
  await expect(page.getByText('Вы в эфире',{exact:true})).toBeVisible({timeout:60000});
  await expect.poll(()=>viewer.evaluate(async()=>{
    for(const pc of window.__testPeers) if(pc.connectionState==='connected') for(const stat of(await pc.getStats()).values()) if(stat.type==='inbound-rtp' && stat.kind==='audio' && stat.totalAudioEnergy>0) return true;
    return false;
  }),{timeout:20000}).toBe(true);
  await page.getByRole('button',{name:'Завершить эфир',exact:true}).click();
  await expect(viewer.getByRole('heading',{name:'native-test сейчас не в эфире'})).toBeVisible({timeout:12000});
  // Screen/system selection remains explicit and independent of application audio.
  const sources=await fetch(origin+'/api/native/sources').then(r=>r.json());
  await page.getByLabel('Окно или экран',{exact:true}).selectOption(sources.find(s=>s.kind==='monitor').id);
  await page.getByLabel('Источник звука',{exact:true}).selectOption('system');
  await expect(page.getByText('Передаётся весь экран и звук всех приложений на основном устройстве вывода.')).toBeVisible();
  await page.getByLabel('Источник звука',{exact:true}).selectOption('off');
  await expect(page.getByLabel('Источник звука',{exact:true})).toHaveValue('off');
  expect(errors).toEqual([]);
  const result={passed:true,checks:['native WGC window → H264 → Go WebRTC → decoded browser video','process WASAPI → PCM → Opus → decoded nonzero audio energy','system WASAPI → PCM → Opus → decoded nonzero audio energy','stop/restart reaches viewer','monitor/system/off selections preserved','no browser errors'],status};
  writeFileSync(resolve(directory,'results.json'),JSON.stringify(result,null,2)); console.log(JSON.stringify(result,null,2));
} finally {
  await browser?.close();
  for(const p of children.reverse()) if(p.exitCode===null) { const done=once(p,'exit'); p.kill(); await done; }
}
