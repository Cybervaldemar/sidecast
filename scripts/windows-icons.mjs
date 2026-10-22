// Regenerate the shared ICO and Go resource objects after changing favicon.svg.
// The generated files are committed, so ordinary builds need no icon tooling.
import { chromium } from '@playwright/test';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { runGo } from './go.mjs';

const chrome = process.env.SIDECAST_CHROME || process.env.LANPLAY_CHROME || (process.platform === 'win32' ? 'C:/Program Files/Google/Chrome/Application/chrome.exe' : '');
const browser = await chromium.launch({ headless: true, ...(chrome && existsSync(chrome) ? { executablePath: chrome } : {}) });
try {
  const page = await browser.newPage({ deviceScaleFactor: 1 });
  const svg = readFileSync('web/public/favicon.svg', 'utf8');
  const sizes = [16, 24, 32, 48, 64, 128, 256];
  const header = Buffer.alloc(6 + sizes.length * 16);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(sizes.length, 4);
  const images = [];
  let offset = header.length;
  for (const [index, size] of sizes.entries()) {
    await page.setViewportSize({ width: size, height: size });
    await page.setContent(`<style>html,body{margin:0;background:transparent}svg{display:block;width:100vw;height:100vh}</style>${svg}`);
    const image = await page.screenshot({ omitBackground: true });
    const entry = 6 + index * 16;
    header[entry] = size % 256;
    header[entry + 1] = size % 256;
    header.writeUInt16LE(1, entry + 4);
    header.writeUInt16LE(32, entry + 6);
    header.writeUInt32LE(image.length, entry + 8);
    header.writeUInt32LE(offset, entry + 12);
    images.push(image);
    offset += image.length;
  }
  writeFileSync('internal/desktop/app.ico', Buffer.concat([header, ...images]));
} finally {
  await browser.close();
}
runGo(['run', 'github.com/tc-hib/go-winres@v0.3.3', 'simply', '--icon', 'internal/desktop/app.ico', '--arch', 'amd64', '--manifest', 'none', '--out', 'cmd/sidecast/rsrc']);
