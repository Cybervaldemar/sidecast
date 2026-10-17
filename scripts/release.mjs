import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { runGo } from './go.mjs';
import { writeNotices } from './notices.mjs';
const targets = process.argv.includes('--current')
  ? [[process.platform === 'win32' ? 'windows' : process.platform, process.arch === 'x64' ? 'amd64' : process.arch]]
  : [['windows','amd64'],['windows','arm64'],['linux','amd64'],['linux','arm64'],['linux','arm'],['linux','mips'],['linux','mipsle']];
mkdirSync('build', { recursive: true });
const checksums = [];
for (const [os, arch] of targets) {
  const path = `build/lanplay-${os}-${arch}${os === 'windows' ? '.exe' : ''}`;
  console.log(`Building ${path}`);
  runGo(['build', '-trimpath', '-ldflags=-s -w', '-o', path, './cmd/lanplay'], { GOOS: os, GOARCH: arch, GOARM: '7', GOMIPS: 'softfloat' });
  checksums.push(`${createHash('sha256').update(readFileSync(path)).digest('hex')}  ${path.slice(6)}`);
}
writeFileSync('build/SHA256SUMS',checksums.join('\n')+'\n');
writeNotices();
