// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLinksValidator from 'starlight-links-validator';
import starlightThemeNova from 'starlight-theme-nova';

export default defineConfig({
	site: 'https://bmux-dev.github.io',
	base: '/bmux',
	trailingSlash: 'always',
	integrations: [
		starlight({
			logo: {
				light: './src/assets/brand/mark-light.svg',
				dark: './src/assets/brand/mark-dark.svg',
				alt: 'bmux',
				replacesTitle: true,
			},
			head: [
				{ tag: 'meta', attrs: { property: 'og:image', content: 'https://bmux-dev.github.io/bmux/social-card.png' } },
				{ tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
				{ tag: 'meta', attrs: { property: 'og:image:height', content: '630' } },
				{ tag: 'meta', attrs: { property: 'og:image:alt', content: 'bmux: A terminal workspace. Already equipped.' } },
				{ tag: 'meta', attrs: { name: 'twitter:image', content: 'https://bmux-dev.github.io/bmux/social-card.png' } },
			],
			title: 'bmux',
			description: 'The batteries-included terminal multiplexer.',
			favicon: '/favicon.svg',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/bmux-dev/bmux' }],
			editLink: { baseUrl: 'https://github.com/bmux-dev/bmux/edit/main/site/' },
			customCss: ['./src/styles/custom.css'],
			plugins: [starlightThemeNova({
					nav: [{ label: 'Docs', href: '/bmux/guides/sessions/' }],
				}), starlightLinksValidator()],
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
