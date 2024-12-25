import { defineCollection } from 'astro:content';
import { docsSchema, i18nSchema } from '@astrojs/starlight/schema';

export const collections = {
	docs: defineCollection({ schema: docsSchema() }),
	// https://starlight.astro.build/guides/i18n/#translate-starlights-ui
	i18n: defineCollection({ type: 'data', schema: i18nSchema() }),
};
