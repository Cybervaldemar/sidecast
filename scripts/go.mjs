import { existsSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
const local = resolve('.tools/go/bin', process.platform === 'win32' ? 'go.exe' : 'go');
export const go = existsSync(local) ? local : 'go';
export const goEnv = { ...process.env, GOPATH: resolve('.tools/gopath'), GOCACHE: resolve('.tools/gocache'), CGO_ENABLED: '0' };
export function runGo(args, extraEnv = {}) {
  mkdirSync('.tools/gopath', { recursive: true }); mkdirSync('.tools/gocache', { recursive: true });
  const result = spawnSync(go, args, { stdio: 'inherit', env: { ...goEnv, ...extraEnv } });
  if (result.error) throw new Error(`Go SDK is required: ${result.error.message}`);
  if (result.status !== 0) process.exit(result.status ?? 1);
}
if (process.argv[1] && resolve(process.argv[1]) === resolve('scripts/go.mjs')) runGo(process.argv.slice(2));
