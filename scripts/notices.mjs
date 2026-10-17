import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { go, goEnv } from './go.mjs';

export function writeNotices() {
  const parts = ['LANPlay third-party license notices\n'];
  function include(name, directory) {
    const license = ['LICENSE','LICENSE.txt','LICENSE.md','COPYING'].map(file => join(directory,file)).find(existsSync);
    if (!license) throw new Error(`No license found for ${name}`);
    parts.push(`\n${'='.repeat(72)}\n${name}\n${'='.repeat(72)}\n${readFileSync(license,'utf8')}`);
  }
  for (const name of ['react','react-dom','scheduler','lucide-react']) include(name,resolve('node_modules',name));
  const result = spawnSync(go,['list','-deps','-f','{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}{{end}}','./cmd/lanplay'],{encoding:'utf8',env:goEnv});
  if (result.status !== 0) throw new Error(result.stderr || 'Cannot list Go dependencies');
  for (const line of new Set(result.stdout.trim().split(/\r?\n/).filter(Boolean))) {
    const [name,version,directory] = line.split('|');
    include(`${name} ${version}`,directory);
  }
  const root = spawnSync(go,['env','GOROOT'],{encoding:'utf8',env:goEnv});
  if (root.status !== 0) throw new Error('Cannot find Go runtime license');
  include('Go runtime and standard library',root.stdout.trim());
  writeFileSync('build/THIRD_PARTY_NOTICES.txt',parts.join('\n'));
  writeFileSync('build/LICENSE',readFileSync('LICENSE'));
}
