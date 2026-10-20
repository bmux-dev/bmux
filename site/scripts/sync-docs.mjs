// Copies the repo's own markdown into the site at build time, so each file
// has one source. Runs before dev and build; the generated pages are
// gitignored.
//   docs/agents.md -> agents/guide.md   (the bmux binary embeds it for `bmux guide`)
//   ROADMAP.md     -> project/roadmap.md
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '../..');
const site = 'https://bmux-dev.github.io/bmux/';

const pages = [
	{
		source: 'docs/agents.md',
		target: 'agents/guide.md',
		frontmatter: [
			'title: Agent guide',
			'description: "How a coding agent finds, reads, drives and watches bmux panes."',
			'editUrl: https://github.com/bmux-dev/bmux/edit/main/docs/agents.md',
			'sidebar:',
			'  order: 5',
		],
		note: 'This is the guide `bmux guide` prints for coding agents, before its command reference.',
	},
	{
		source: 'ROADMAP.md',
		target: 'project/roadmap.md',
		frontmatter: [
			'title: Roadmap',
			'description: "What bmux has shipped, what is next, and what is deliberately left out."',
			'editUrl: https://github.com/bmux-dev/bmux/edit/main/ROADMAP.md',
			'sidebar:',
			'  order: 1',
		],
	},
];

for (const { source, target, frontmatter, note } of pages) {
	const sourcePath = resolve(repo, source);
	const targetPath = resolve(here, '../src/content/docs', target);

	const text = await readFile(sourcePath, 'utf8');
	const heading = text.match(/^# .+\n+/);
	if (!heading) throw new Error(`${sourcePath} must start with a "# " heading`);

	// Links to the published site become site-relative so the link validator checks them.
	const body = text.slice(heading[0].length).replaceAll(`](${site}`, '](/bmux/');

	const page = ['---', ...frontmatter, '---', '', ...(note ? [`:::note`, note, ':::', ''] : []), body].join('\n');

	await mkdir(dirname(targetPath), { recursive: true });
	await writeFile(targetPath, page);
}
