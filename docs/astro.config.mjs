import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://www.chproxy.org',
	integrations: [
		starlight({
			title: 'Chproxy',
			description: 'Chproxy is an HTTP proxy and load balancer for ClickHouse',
			social: {
				github: 'https://github.com/ContentSquare/chproxy',
				twitter: 'https://twitter.com/contentsquarerd'
			},
			editLink: {
				baseUrl: 'https://github.com/ContentSquare/chproxy/edit/master/docs/',
			},
			logo: {
				dark: './src/assets/logo-white.svg',
				light: './src/assets/logo-black.svg',
				replacesTitle: true,
			},
			customCss: [
				// Relative path to your custom CSS file
				'./src/styles/custom.css',
			],
			defaultLocale: 'root',
			locales: {
				// English docs in `src/content/docs/en/`
				root: {
					label: 'English',
					lang: 'en'
				},
				// Simplified Chinese docs in `src/content/docs/zh/`
				cn: {
					label: '简体中文',
					lang: 'zh-CN',
				}
			},
			sidebar: [
				{
					label: 'Guides',
					items: [
						{ label: 'Introduction', link: '/' },
						{ label: 'Installation', link: '/install/' },
						{ label: 'Quick start', link: '/getting_started/' },
						{ label: 'Changelog', link: '/changelog/' },
						{ label: 'History', link: '/history/' },
						{ label: 'FAQ', link: '/faq/' }
					],
				},
				{
					label: 'Configuration',
					autogenerate: { directory: 'configuration' }
				},
				{
					label: 'Use cases',
					autogenerate: { directory: 'use-cases' }
				},
			],
		}),
	],
});
