import theme from '@nuxt/content-theme-docs'

export default theme({
  css: [
    `${__dirname}/assets/css/main.css`
  ],
  head: {
    link: [
      { rel: 'icon', type: 'image/x-icon', href: '/favicon.svg' }
    ]
  },
  i18n: {
    locales: () => [{
      code: 'cn',
      iso: 'zh-cn',
      file: 'zh-CN.js',
      name: '中文'
    }, {
      code: 'en',
      iso: 'en-US',
      file: 'en-US.js',
      name: 'English'
    }],
    defaultLocale: 'en'
  }
})
