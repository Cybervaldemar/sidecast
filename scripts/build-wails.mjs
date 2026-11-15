import { mkdirSync } from 'node:fs';
import { runGo } from './go.mjs';

if (process.platform !== 'win32' || process.arch !== 'x64') {
  throw new Error('The Wails desktop build currently targets Windows x64.');
}

const directory = 'build/windows-amd64';
const output = `${directory}/sidecast-wails-windows-amd64.exe`;
mkdirSync(directory, { recursive: true });
runGo([
  'build',
  '-trimpath',
  '-tags',
  'wails,desktop,production',
  '-ldflags',
  '-s -w -H=windowsgui',
  '-o',
  output,
  './cmd/sidecast',
]);
console.log(`Built ${output}`);
