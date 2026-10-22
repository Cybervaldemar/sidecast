import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { runGo } from './go.mjs';
import { writeNotices } from './notices.mjs';
import { cleanBuild } from './clean-build.mjs';
const supportedTargets = [['windows', 'amd64'], ['linux', 'amd64']];
const currentOS = process.platform === 'win32' ? 'windows' : process.platform;
const currentArch = process.arch === 'x64' ? 'amd64' : process.arch;
const targets = process.argv.includes('--current')
  ? supportedTargets.filter(([os, arch]) => os === currentOS && arch === currentArch)
  : supportedTargets;
if (!targets.length) throw new Error('Only Windows x64 and Linux x64 builds are supported. Use npm run build:all to cross-build both.');
// Every build is a complete output set: no stale binaries from previous targets.
cleanBuild();
mkdirSync('build', { recursive: true });
const checksums = [];
for (const [os, arch] of targets) {
  const directory = `build/${os}-${arch}`;
  mkdirSync(directory, { recursive: true });
  const path = `${directory}/sidecast-${os}-${arch}${os === 'windows' ? '.exe' : ''}`;
  console.log(`Building ${path}`);
  const flags = `-s -w${os === 'windows' ? ' -H=windowsgui' : ''}`;
  runGo(['build', '-trimpath', `-ldflags=${flags}`, '-o', path, './cmd/sidecast'], { GOOS: os, GOARCH: arch });
  checksums.push(`${createHash('sha256').update(readFileSync(path)).digest('hex')}  ${path.slice(6)}`);
}
writeFileSync('build/SHA256SUMS',checksums.join('\n')+'\n');
writeNotices();
