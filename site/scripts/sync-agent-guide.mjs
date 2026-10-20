// Copies docs/agents.md, which the bmux binary embeds for `bmux guide`, into
// the site so the agent guide has one source. Runs before dev and build.
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const source = resolve(here, '../../docs/agents.md');
const target = resolve(here, '../src/content/docs/agents/guide.md');

const text = await readFile(source, 'utf8');
const heading = text.match(/^# .+\n+/);
if (!heading) throw new Error(`${source} must start with a "# " heading`);

const page = [
	'---',
	'title: Agent guide',
	'description: "How a coding agent finds, reads, drives and watches bmux panes."',
	'editUrl: https://github.com/bmux-dev/bmux/edit/main/docs/agents.md',
	'sidebar:',
	'  order: 5',
	'---',
	'',
	':::note',
	'This is the guide `bmux guide` prints for coding agents, before its command reference.',
	':::',
	'',
	text.slice(heading[0].length),
].join('\n');

await mkdir(dirname(target), { recursive: true });
await writeFile(target, page);
