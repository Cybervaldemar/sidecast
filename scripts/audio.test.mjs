import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

// Run the browser-independent SDP helper directly from the production source.
const source = readFileSync(new URL('../web/src/lib/audio.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
}).outputText;
const { withGameAudio } = await import(`data:text/javascript,${encodeURIComponent(compiled)}`);

test('stereo offer preserves nondefault Opus payload, fallback audio and unrelated video', () => {
  const video =
    'm=video 9 UDP/TLS/RTP/SAVPF 111\r\na=rtpmap:111 H264/90000\r\na=fmtp:111 profile-level-id=42e01f\r\n';
  const original = {
    type: 'offer',
    sdp:
      'v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 109 0\r\na=rtpmap:109 opus/48000/2\r\na=rtpmap:0 PCMU/8000\r\na=fmtp:109 minptime=10;useinbandfec=1;stereo=0;maxaveragebitrate=32000;usedtx=1\r\n' +
      video,
  };
  const result = withGameAudio(original);
  const opus = result.sdp.match(/^a=fmtp:109 ([^\r\n]*)/m)[1];
  const parameters = opus.split(';').map((parameter) => parameter.split('='));
  const values = Object.fromEntries(parameters);
  assert.equal(result.type, 'offer');
  assert.equal(values.stereo, '1');
  assert.equal(values['sprop-stereo'], '1');
  assert.equal(values.maxaveragebitrate, '256000');
  assert.equal(values.maxplaybackrate, '48000');
  assert.equal(values.useinbandfec, '1');
  assert.equal(values.usedtx, '0');
  assert.equal(values.minptime, '10');
  assert.equal(parameters.length, Object.keys(values).length, 'duplicate parameters');
  assert.ok(result.sdp.endsWith(video));
  assert.ok(result.sdp.includes('m=audio 9 UDP/TLS/RTP/SAVPF 109 0\r\n'));
  assert.ok(result.sdp.includes('a=rtpmap:0 PCMU/8000\r\n'));
  assert.ok(original.sdp.includes('stereo=0'), 'input was mutated');
  assert.deepEqual(withGameAudio(result), result, 'applying twice changed the offer');
});

test('stereo offer adds missing Opus parameters within the audio section', () => {
  const result = withGameAudio({
    type: 'offer',
    sdp: 'v=0\nm=audio 9 UDP/TLS/RTP/SAVPF 103\na=rtpmap:103 opus/48000/2\nm=video 9 UDP/TLS/RTP/SAVPF 96\na=rtpmap:96 VP8/90000\n',
  });
  assert.match(result.sdp, /a=rtpmap:103 opus\/48000\/2\na=fmtp:103 [^\n]*stereo=1[^\n]*\nm=video/);
  assert.ok(!result.sdp.includes('\r'), 'line ending style changed');
});

test('offers with no Opus keep their fallback codecs unchanged', () => {
  const description = {
    type: 'offer',
    sdp: 'v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=rtpmap:0 PCMU/8000\r\n',
  };
  assert.deepEqual(withGameAudio(description), description);
  assert.deepEqual(withGameAudio({ type: 'offer' }), { type: 'offer' });
});
