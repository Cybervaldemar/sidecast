import { mkdirSync, readFileSync } from 'node:fs';
import { runGo } from './go.mjs';
import { pathToFileURL } from 'node:url';

export function buildWails(output, version, extraEnv = {}) {
  mkdirSync('build/windows-amd64', { recursive: true });
  runGo([
    'build',
    '-trimpath',
    '-tags',
    'wails,desktop,production',
    '-ldflags',
    `-s -w -H=windowsgui -X sidecast/internal/update.Version=${version}`,
    '-o',
    output,
    './cmd/sidecast',
  ], { GOOS: 'windows', GOARCH: 'amd64', ...extraEnv });
  console.log(`Built ${output}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const version = JSON.parse(readFileSync('package.json', 'utf8')).version;
  buildWails('build/windows-amd64/sidecast-windows-amd64.exe', version);
}
