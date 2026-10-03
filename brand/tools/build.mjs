// Writes the sous brand kit into brand/ from brand-art.mjs, the way
// shellbell's kit is made: the mark drawn once, the wordmark outlined from
// JetBrains Mono Bold (no live text, no font needed to view a logo), every
// raster rendered from those vectors. Run: cd brand/tools && npm i && npm run brand
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import opentype from 'opentype.js';
import sharp from 'sharp';
import { centeredMark, gloss, glossDefs, icon, INK, mark, PAPER, plate, svg, VIOLET, VIOLET_DEEP } from './brand-art.mjs';

const brand = join(dirname(fileURLToPath(import.meta.url)), '..');
const at = (...p) => {
  const f = join(brand, ...p);
  mkdirSync(dirname(f), { recursive: true });
  return f;
};
const write = (f, body) => writeFileSync(at(f), body);
const png = (svgText, f, opts) => sharp(Buffer.from(svgText)).resize(opts).png().toFile(at(f));

// The wordmark: lowercase sous, outlined.
// JetBrains Mono Bold, from npm: the font is only needed to make the outlines.
const ttf = readFileSync(
  fileURLToPath(import.meta.resolve('@fontsource/jetbrains-mono/files/jetbrains-mono-latin-700-normal.woff')),
);
const font = opentype.parse(ttf.buffer.slice(ttf.byteOffset, ttf.byteOffset + ttf.byteLength));
const word = new opentype.Path();
let pen = 0;
for (const letter of 'sous') {
  const glyph = font.charToGlyph(letter);
  word.extend(glyph.getPath(pen, 0, 220));
  pen += (glyph.advanceWidth * 220) / font.unitsPerEm;
}
const bb = word.getBoundingBox();
const width = bb.x2 - bb.x1;
const height = bb.y2 - bb.y1;
const wordAt = (x, y, color) =>
  `<g transform="translate(${x - bb.x1} ${y - bb.y1})"><path d="${word.toPathData(3)}" fill="${color}"/></g>`;

// The logo family: four compositions in four colourways, as shellbell's.
const colourways = [
  ['on-dark', PAPER, VIOLET],
  ['on-light', INK, VIOLET_DEEP],
  ['black', INK, INK],
  ['white', '#FFFFFF', '#FFFFFF'],
];
const family = {};
for (const [way, ink, accent] of colourways) {
  family[`mark-${way}`] = svg('sous mark', mark(accent), '-20 -20 400 320');
  family[`wordmark-${way}`] = svg('sous wordmark', wordAt(20, 20, ink), `0 0 ${width + 40} ${height + 40}`);
  family[`horizontal-${way}`] = svg(
    'sous horizontal logo',
    `<g transform="translate(20 20) scale(0.68)">${mark(accent)}</g>` + wordAt(310, 20 + (190.4 - height) / 2, ink),
    `0 0 ${width + 330} 230.4`,
  );
  family[`stacked-${way}`] = svg(
    'sous stacked logo',
    `<g transform="translate(${(Math.max(width, 360) + 40 - 360) / 2} 20)">${mark(accent)}</g>` +
      wordAt(20 + (Math.max(width, 360) - width) / 2, 370, ink),
    `0 0 ${Math.max(width, 360) + 40} ${height + 390}`,
  );
}
for (const [name, body] of Object.entries(family)) {
  write(`svg/${name}.svg`, body);
  await png(body, `png/${name}@1x.png`, { height: 128 });
  await png(body, `png/${name}@2x.png`, { height: 256 });
}

// Icons: glossy violet on shellbell's ink plate; flat for where gloss is wrong.
const icons = {
  'icon-gloss-rounded': icon({ rounded: true }),
  'icon-gloss-square': icon({ rounded: false }),
  'icon-flat-rounded': icon({ rounded: true, flat: true }),
};
for (const [name, body] of Object.entries(icons)) {
  write(`icons/${name}.svg`, body);
  await png(body, `icons/${name}-1024.png`, { width: 1024, height: 1024 });
}

// Web: what a site needs.
write('web/favicon.svg', icons['icon-gloss-rounded']);
await png(icons['icon-gloss-square'], 'web/apple-touch-icon.png', { width: 180, height: 180 }); // iOS rounds it
for (const s of [16, 32, 48, 192, 512]) await png(icons['icon-gloss-rounded'], `web/icon-${s}.png`, { width: s, height: s });

// The social card: the icon, the name, the promise, on ink.
const tile = `<svg x="80" y="80" width="112" height="112" viewBox="0 0 1024 1024">${glossDefs + plate({ rounded: true }) + centeredMark(VIOLET) + gloss(true)}</svg>`;
const name = `<g transform="translate(222 98) scale(0.34)">${wordAt(0, 0, PAPER)}</g>`;
const social = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 630">
  <defs><radialGradient id="glow" cx="0.85" cy="0.05" r="0.85"><stop offset="0" stop-color="#8B5CF6" stop-opacity=".42"/><stop offset="1" stop-color="#08090D" stop-opacity="0"/></radialGradient></defs>
  <rect width="1200" height="630" fill="#08090D"/><rect width="1200" height="630" fill="url(#glow)"/>
  ${tile}${name}
  <text x="80" y="335" font-family="Manrope, Helvetica Neue, Arial, sans-serif" font-size="60" font-weight="700" fill="${PAPER}" letter-spacing="-1.5">Everything waiting on you,</text>
  <text x="80" y="410" font-family="Manrope, Helvetica Neue, Arial, sans-serif" font-size="60" font-weight="700" fill="${VIOLET}" letter-spacing="-1.5">in one list.</text>
  <text x="80" y="545" font-family="JetBrains Mono, Menlo, monospace" font-size="27" fill="#A4A6B2">brew install bilal-/tap/sous</text>
  <text x="1120" y="545" text-anchor="end" font-family="JetBrains Mono, Menlo, monospace" font-size="27" fill="#6F7180">sous.bilal.sh</text>
</svg>
`;
write('social-card.svg', social);
await sharp(Buffer.from(social), { density: 150 }).resize(1200, 630).png().toFile(at('social-card.png'));

// The preview: the kit on one sheet, at the sizes it is seen.
const cells = [];
const place = async (body, left, top, size, opts = {}) => {
  const buf = await sharp(Buffer.from(body)).resize({ height: size, ...opts }).png().toBuffer();
  cells.push({ input: buf, left, top });
};
await place(family['wordmark-on-light'], 72, 56, 44);
await place(icons['icon-gloss-rounded'], 72, 150, 300);
await place(family['horizontal-on-light'], 440, 175, 84);
await place(family['stacked-on-light'], 440, 320, 150);
const dark = await sharp({ create: { width: 560, height: 300, channels: 4, background: '#08090D' } })
  .composite([
    { input: await sharp(Buffer.from(family['horizontal-on-dark'])).resize({ height: 90 }).png().toBuffer(), left: 50, top: 50 },
    { input: await sharp(Buffer.from(family['mark-white'])).resize({ height: 40 }).png().toBuffer(), left: 50, top: 200 },
    { input: await sharp(Buffer.from(family['mark-on-dark'])).resize({ height: 40 }).png().toBuffer(), left: 130, top: 200 },
  ])
  .png()
  .toBuffer();
cells.push({ input: dark, left: 808, top: 150 });
let x = 72;
for (const s of [16, 24, 32, 48, 64, 128]) {
  await place(icons['icon-gloss-rounded'], x, 640 - s / 2, s, { width: s });
  x += s + 40;
}
const label = (t, x, y, size = 15, fill = '#5B5E68') =>
  `<text x="${x}" y="${y}" font-family="Helvetica Neue, Arial" font-size="${size}" fill="${fill}">${t}</text>`;
const sheet = `<svg xmlns="http://www.w3.org/2000/svg" width="1440" height="820">
  <rect width="1440" height="820" fill="${PAPER}"/>
  ${label('BRAND KIT · SIBLING OF SHELLBELL', 72, 122)}
  ${label('ICON', 72, 480)}${label('HORIZONTAL · STACKED', 440, 520)}${label('ON DARK · MARK WHITE / VIOLET', 808, 480)}
  ${label('SMALL SIZES · 16 24 32 48 64 128', 72, 760)}
  ${label('VIOLET #A78BFA · DEEP #7C3AED · INK #17191D · PAPER #F8F7F4', 808, 640, 15, INK)}
  ${label('JetBrains Mono Bold wordmark · Manrope for copy', 808, 670)}
</svg>`;
await sharp(Buffer.from(sheet)).composite(cells).png().toFile(at('preview.png'));

console.log('brand: 16 logos (svg + png), 3 icons, web icons, social card, preview');
