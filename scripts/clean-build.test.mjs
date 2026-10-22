import { test } from 'node:test';
import assert from 'node:assert/strict';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';

async function fixture(t) {
  const temporary = resolve(tmpdir());
  const root = mkdtempSync(join(temporary, 'sidecast-clean-test-'));
  t.after(() => {
    assert.ok(root.startsWith(temporary + sep));
    rmSync(root, { recursive: true, force: true });
  });
  mkdirSync(join(root, 'scripts'));
  copyFileSync(new URL('./clean-build.mjs', import.meta.url), join(root, 'scripts/clean-build.mjs'));
  const { cleanBuild } = await import(pathToFileURL(join(root, 'scripts/clean-build.mjs')));
  const build = join(root, 'build');
  mkdirSync(build);
  const put = (path, content = 'fixture') => {
    const target = join(build, path);
    mkdirSync(resolve(target, '..'), { recursive: true });
    writeFileSync(target, content);
    return target;
  };
  return { root, build, put, cleanBuild };
}

test('removes generated artifacts while preserving storage and unknown files', async t => {
  const { build, put, cleanBuild } = await fixture(t);
  const artifacts = ['studio/windows-amd64/sidecast-studio.exe', 'native/sidecast-audio.exe', 'optimized/sidecast.exe', 'windows-amd64/sidecast-windows-amd64.exe', 'windows-arm64/sidecast-windows-arm64.exe', 'linux-arm64/sidecast-linux-arm64', 'linux-arm/sidecast-linux-arm', 'linux-mips/sidecast-linux-mips', 'linux-mipsle/sidecast-linux-mipsle', 'sidecast-linux-amd64', 'lanplay-windows-amd64.exe', 'lanplay-linux-amd64', 'SHA256SUMS'];
  for (const path of artifacts) put(path);
  const storage = put('data/channels.json', 'saved channels');
  const unknown = put('notes.txt', 'keep');
  cleanBuild();
  for (const path of artifacts) assert.equal(existsSync(join(build, path)), false);
  assert.equal(readFileSync(storage, 'utf8'), 'saved channels');
  assert.equal(readFileSync(unknown, 'utf8'), 'keep');
});

test('storage inside an artifact directory prevents any deletion', async t => {
  const { put, cleanBuild } = await fixture(t);
  const binary = put('sidecast-linux-amd64');
  const storage = put('studio/windows-amd64/Data/CHANNELS.JSON', 'saved channels');
  assert.throws(cleanBuild, /Persistent data found/);
  assert.equal(existsSync(binary), true);
  assert.equal(readFileSync(storage, 'utf8'), 'saved channels');
});

test('removes empty legacy data directories', async t => {
  const { build, cleanBuild } = await fixture(t);
  mkdirSync(join(build, 'data'));
  cleanBuild();
  assert.equal(existsSync(join(build, 'data')), false);
});

test('does not follow directory links outside build', async t => {
  const { root, build, cleanBuild } = await fixture(t);
  const external = join(root, 'outside');
  mkdirSync(external);
  writeFileSync(join(external, 'keep.txt'), 'keep');
  try {
    symlinkSync(external, join(build, 'studio'), process.platform === 'win32' ? 'junction' : 'dir');
  } catch (error) {
    if (error.code === 'EPERM') return t.skip('This Windows session cannot create directory links');
    throw error;
  }
  cleanBuild();
  assert.equal(existsSync(join(build, 'studio')), false);
  assert.equal(readFileSync(join(external, 'keep.txt'), 'utf8'), 'keep');
});
