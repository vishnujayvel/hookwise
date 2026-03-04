import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Hookwise',
  description: 'Smart hooks framework for Claude Code',
  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Reference', link: '/reference/cli-reference' },
      { text: 'GitHub', link: 'https://github.com/vishnujayvel/hookwise' }
    ],
    sidebar: {
      '/guide/': [
        {
          text: 'Guide',
          items: [
            { text: 'Getting Started', link: '/guide/getting-started' },
            { text: 'Feeds Guide', link: '/guide/feeds-guide' },
            { text: 'Analytics Guide', link: '/guide/analytics-guide' },
            { text: 'Creating a Recipe', link: '/guide/creating-a-recipe' }
          ]
        }
      ],
      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'CLI Reference', link: '/reference/cli-reference' },
            { text: 'Hook Events', link: '/reference/hook-events' },
            { text: 'TUI Guide', link: '/reference/tui-guide' },
            { text: 'Troubleshooting', link: '/reference/troubleshooting' },
            { text: 'Migration', link: '/reference/migration' }
          ]
        }
      ],
      '/features/': [
        {
          text: 'Features',
          items: [
            { text: 'Guards', link: '/features/guards' },
            { text: 'Analytics', link: '/features/analytics' },
            { text: 'Feeds', link: '/features/feeds' },
            { text: 'Status Line', link: '/features/status-line' },
            { text: 'Coaching', link: '/features/coaching' }
          ]
        }
      ],
      '/': [
        {
          text: 'About',
          items: [
            { text: 'Architecture', link: '/architecture' },
            { text: 'Philosophy', link: '/philosophy' }
          ]
        }
      ]
    }
  }
})
