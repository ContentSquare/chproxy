# Chproxy documentation

The documentation website is build with Nuxt and the official docs theme.

It is automatically deployed on each commit on the `main` branch, through
Render.

# CONTRIBUTING

## Ordering links in the sidebar

Each page should be given a unique `position`, that will determine where it will
be positioned in the sidebar. Each page can also have a `category`, to group
similar pages together.

Note that the `position` is relative to the top of the sidebar, not to the top
of the parent category.

**Tip:** Span `position` values on various hundreds, so you can more easily
reorder links in a given category. For example, all links in the first category
should be `101`, `102`, etc and links in the third category should be `301`,
`302`, etc.



## TODO:

- Configure Render for deployment 
- Configure Render for Pull Requests
  - Can we make it smart and only re-render if changes in the docs, not if
    changes in the code?
- Configure chproxy.org
- Configure Algolia DocSearch
- Need to add a logo
- Replace the manual copy/paste of files with dynamic links
