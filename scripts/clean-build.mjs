import { existsSync, lstatSync, readdirSync, rmSync } from 'node:fs';
import { dirname, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const workspace = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const build = resolve(workspace, 'build');

function checkNoStorage(directory) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const name = entry.name.toLowerCase();
    if (name === 'channels.json' || (name === 'data' && entry.isDirectory() && readdirSync(resolve(directory, entry.name)).length)) {
      throw new Error(`Persistent data found in ${directory}. Move it outside build/ before cleaning.`);
    }
    if (entry.isDirectory() && !entry.isSymbolicLink()) checkNoStorage(resolve(directory, entry.name));
  }
}

export function cleanBuild() {
  if (!existsSync(build)) return;
  if (lstatSync(build).isSymbolicLink()) throw new Error('Refusing to clean a linked build directory');
  const targets = [];
  for (const entry of readdirSync(build, { withFileTypes: true })) {
    // Also remove legacy ARM, MIPS and macOS artifacts from older builds.
    const generated = /^(windows|linux|darwin)-(amd64|arm64|arm|mips|mipsle)$/.test(entry.name)
      || /^(native|optimized|studio|LICENSE|SHA256SUMS|THIRD_PARTY_NOTICES\.txt)$/.test(entry.name)
      || /^(sidecast|lanplay)-(windows|linux|darwin)-[a-z0-9]+(\.exe)?$/.test(entry.name);
    const emptyData = entry.name === 'data' && entry.isDirectory() && readdirSync(resolve(build, entry.name)).length === 0;
    if (!generated && !emptyData) continue;
    const target = resolve(build, entry.name);
    if (!target.startsWith(build + sep) || !build.startsWith(workspace + sep)) throw new Error('Build cleanup escaped the workspace');
    if (entry.isDirectory() && !entry.isSymbolicLink()) checkNoStorage(target);
    targets.push({ target, recursive: !entry.isSymbolicLink() });
  }
  // Validate all targets before removing anything. Persistent build/data is
  // preserved, and junctions/symlinks are removed without following them.
  for (const { target, recursive } of targets) {
    rmSync(target, { recursive, force: true });
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) cleanBuild();
