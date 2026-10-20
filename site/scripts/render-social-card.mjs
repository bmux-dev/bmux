// Renders src/assets/brand/social-card.svg to public/social-card.png. Social
// sites don't show SVG link previews, so the site's og:image is this PNG.
// Run it after editing the SVG: npm run social-card
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import sharp from 'sharp';

const here = dirname(fileURLToPath(import.meta.url));
const source = resolve(here, '../src/assets/brand/social-card.svg');
const target = resolve(here, '../public/social-card.png');

await sharp(source, { density: 72 }).png().toFile(target);
