// BLAKE2b-512(RFC 7693。鍵なし)。minisign の ED 方式は対象の BLAKE2b-512 に署名するが、Electron の Node(BoringSSL)は
// `createHash("blake2b512")` を持たない("Digest method not supported")ため、署名の検証で使う分をここで自前に持つ。
// 64 ビットの語は下位・上位の 32 ビットの対で扱う(BigInt より速い)。Node の OpenSSL の実装との一致は app/test/u3a/mp-blake2b.test.ts が確かめる。
const IV = [0xf3bcc908, 0x6a09e667, 0x84caa73b, 0xbb67ae85, 0xfe94f82b, 0x3c6ef372, 0x5f1d36f1, 0xa54ff53a, 0xade682d1, 0x510e527f, 0x2b3e6c1f, 0x9b05688c, 0xfb41bd6b, 0x1f83d9ab, 0x137e2179, 0x5be0cd19];
const SIGMA = [
  [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15], [14, 10, 4, 8, 9, 15, 13, 6, 1, 12, 0, 2, 11, 7, 5, 3],
  [11, 8, 12, 0, 5, 2, 15, 13, 10, 14, 3, 6, 7, 1, 9, 4], [7, 9, 3, 1, 13, 12, 11, 14, 2, 6, 5, 10, 4, 0, 15, 8],
  [9, 0, 5, 7, 2, 4, 10, 15, 14, 1, 11, 12, 6, 8, 3, 13], [2, 12, 6, 10, 0, 11, 8, 3, 4, 13, 7, 5, 15, 14, 1, 9],
  [12, 5, 1, 15, 14, 13, 4, 10, 0, 7, 6, 3, 9, 2, 8, 11], [13, 11, 7, 14, 12, 1, 3, 9, 5, 0, 15, 4, 8, 6, 2, 10],
  [6, 15, 14, 9, 11, 3, 0, 8, 12, 2, 13, 7, 1, 4, 10, 5], [10, 2, 8, 4, 7, 6, 1, 5, 15, 11, 9, 14, 3, 12, 13, 0],
];
const BLOCK = 128;
const OUT = 64;

/** v[a] = v[a] + v[b] + m(下位・上位の対)。 */
function add3(v: Uint32Array, a: number, b: number, m: Uint32Array, x: number): void {
  const lo = v[2 * a]! + v[2 * b]! + m[2 * x]!;
  const carry = Math.floor(lo / 4294967296);
  v[2 * a] = lo >>> 0;
  v[2 * a + 1] = (v[2 * a + 1]! + v[2 * b + 1]! + m[2 * x + 1]! + carry) >>> 0;
}
function add2(v: Uint32Array, a: number, b: number): void {
  const lo = v[2 * a]! + v[2 * b]!;
  v[2 * a] = lo >>> 0;
  v[2 * a + 1] = (v[2 * a + 1]! + v[2 * b + 1]! + (lo > 0xffffffff ? 1 : 0)) >>> 0;
}
/** v[d] = (v[d] ^ v[a]) を右へ n ビット回す(n は 32・24・16・63)。 */
function xorRot(v: Uint32Array, d: number, a: number, n: number): void {
  const lo = v[2 * d]! ^ v[2 * a]!;
  const hi = v[2 * d + 1]! ^ v[2 * a + 1]!;
  if (n === 32) { v[2 * d] = hi >>> 0; v[2 * d + 1] = lo >>> 0; } else if (n === 63) {
    v[2 * d] = ((lo << 1) | (hi >>> 31)) >>> 0; v[2 * d + 1] = ((hi << 1) | (lo >>> 31)) >>> 0;
  } else {
    v[2 * d] = ((lo >>> n) | (hi << (32 - n))) >>> 0; v[2 * d + 1] = ((hi >>> n) | (lo << (32 - n))) >>> 0;
  }
}

function g(v: Uint32Array, m: Uint32Array, a: number, b: number, c: number, d: number, x: number, y: number): void {
  add3(v, a, b, m, x); xorRot(v, d, a, 32);
  add2(v, c, d); xorRot(v, b, c, 24);
  add3(v, a, b, m, y); xorRot(v, d, a, 16);
  add2(v, c, d); xorRot(v, b, c, 63);
}

function compress(h: Uint32Array, block: Buffer, t: number, last: boolean): void {
  const v = new Uint32Array(32);
  const m = new Uint32Array(32);
  v.set(h, 0);
  v.set(IV, 16);
  v[24] = (v[24]! ^ (t >>> 0)) >>> 0;
  v[25] = (v[25]! ^ Math.floor(t / 4294967296)) >>> 0;
  if (last) { v[28] = (~v[28]!) >>> 0; v[29] = (~v[29]!) >>> 0; }
  for (let i = 0; i < 32; i++) m[i] = block.readUInt32LE(i * 4);
  for (let r = 0; r < 12; r++) {
    const s = SIGMA[r % 10]!;
    g(v, m, 0, 4, 8, 12, s[0]!, s[1]!); g(v, m, 1, 5, 9, 13, s[2]!, s[3]!);
    g(v, m, 2, 6, 10, 14, s[4]!, s[5]!); g(v, m, 3, 7, 11, 15, s[6]!, s[7]!);
    g(v, m, 0, 5, 10, 15, s[8]!, s[9]!); g(v, m, 1, 6, 11, 12, s[10]!, s[11]!);
    g(v, m, 2, 7, 8, 13, s[12]!, s[13]!); g(v, m, 3, 4, 9, 14, s[14]!, s[15]!);
  }
  for (let i = 0; i < 16; i++) h[i] = (h[i]! ^ v[i]! ^ v[i + 16]!) >>> 0;
}

export function blake2b512(data: Uint8Array): Buffer {
  const input = Buffer.from(data.buffer, data.byteOffset, data.byteLength);
  const h = new Uint32Array(IV.slice(0, 16));
  h[0] = (h[0]! ^ 0x01010000 ^ OUT) >>> 0;
  let at = 0;
  while (input.length - at > BLOCK) {
    compress(h, input.subarray(at, at + BLOCK), at + BLOCK, false);
    at += BLOCK;
  }
  const lastBlock = Buffer.alloc(BLOCK);
  input.copy(lastBlock, 0, at);
  compress(h, lastBlock, input.length, true);
  const out = Buffer.alloc(OUT);
  for (let i = 0; i < 16; i++) out.writeUInt32LE(h[i]!, i * 4);
  return out;
}
