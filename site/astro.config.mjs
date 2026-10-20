// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLinksValidator from 'starlight-links-validator';

export default defineConfig({
	site: 'https://bmux-dev.github.io',
	base: '/bmux',
	trailingSlash: 'always',
	integrations: [
		starlight({
			title: 'bmux',
			description: 'The batteries-included terminal multiplexer.',
			favicon: '/favicon.svg',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/bmux-dev/bmux' }],
			editLink: { baseUrl: 'https://github.com/bmux-dev/bmux/edit/main/site/' },
			customCss: ['./src/styles/custom.css'],
			plugins: [starlightLinksValidator()],
			sidebar: [
				{ label: 'Start here', items: [{ autogenerate: { directory: 'start' } }] },
				{ label: 'Using bmux', items: [{ autogenerate: { directory: 'guides' } }] },
				{ label: 'Agents', items: [{ autogenerate: { directory: 'agents' } }] },
				{ label: 'Configuration', items: [{ autogenerate: { directory: 'configuration' } }] },
				{ label: 'Reference', items: [{ autogenerate: { directory: 'reference' } }] },
				{ label: 'Project', items: [{ autogenerate: { directory: 'project' } }] },
			],
		}),
	],
});
