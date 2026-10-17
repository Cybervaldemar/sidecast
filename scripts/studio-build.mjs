import { copyFileSync, existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { runGo } from './go.mjs';
import { writeNotices } from './notices.mjs';

const targets = process.argv.includes('--all') ? [['windows','amd64'],['linux','amd64'],['linux','arm64']] : [[process.platform === 'win32' ? 'windows' : process.platform, process.arch === 'x64' ? 'amd64' : process.arch]];
mkdirSync('build', {recursive:true}); writeNotices();
for (const [os, arch] of targets) {
  const directory = resolve('build/studio', `${os}-${arch}`); mkdirSync(directory, {recursive:true});
  runGo(['build','-trimpath','-ldflags=-s -w','-o',join(directory,`lanplay-studio${os === 'windows' ? '.exe' : ''}`),'./cmd/lanplay-studio'],{GOOS:os,GOARCH:arch});
  if (os === 'windows' && arch === 'amd64') {
    if (process.platform !== 'win32') throw new Error('Build the Windows audio adapter on Windows using the .NET Framework C# compiler.');
    const compiler = join(process.env.WINDIR || 'C:/Windows','Microsoft.NET','Framework64','v4.0.30319','csc.exe');
    const result = spawnSync(compiler,['/nologo','/optimize+','/platform:x64','/target:exe',`/out:${join(directory,'lanplay-audio.exe')}`,resolve('native/windows/AudioCapture.cs')],{stdio:'inherit',windowsHide:true});
    if (result.status !== 0) throw new Error('Cannot build WASAPI adapter: install .NET Framework 4.x developer tools.');
    // FFmpeg is an external runtime, not linked to or downloaded by the app.
    // Keep distribution separate; users may install an appropriate build and
    // pass --ffmpeg, or place their own ffmpeg.exe beside the studio executable.
    writeFileSync(join(directory,'START.cmd'),'@echo off\r\ncd /d "%~dp0"\r\nlanplay-studio.exe %*\r\npause\r\n');
  }
  copyFileSync('LICENSE',join(directory,'LICENSE'));
  copyFileSync('build/THIRD_PARTY_NOTICES.txt',join(directory,'THIRD_PARTY_NOTICES.txt'));
  writeFileSync(join(directory,'README.txt'),`LANPlay Studio\n\nStart with: lanplay-studio${os === 'windows' ? '.exe' : ''} --server http://YOUR_SERVER:8080\nOpen http://127.0.0.1:8765 in your browser.\n\nWindows x64: native WGC video + WASAPI application/system audio.\nRequires a separately installed FFmpeg build with gfxcapture and libopus.\nUse --ffmpeg PATH, PATH lookup, or put ffmpeg.exe beside this executable.\nThe bundled lanplay-audio.exe uses Windows .NET Framework 4.x.\n\nLinux: existing browser screen/audio capture, no native capture dependency.\nFPS and audio-source availability depend on browser, OS, GPU and network.\nSee the project README for limitations and redistribution details.\n`);
  console.log(`Studio built: ${directory}`);
}
