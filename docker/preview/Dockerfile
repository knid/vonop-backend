FROM node:20-alpine

# Install bun, set up proxy server, then clean up npm/yarn/corepack
RUN npm install -g bun && \
    rm -rf /root/.npm /root/.cache /opt/yarn* && \
    rm -f /usr/local/bin/yarn /usr/local/bin/yarnpkg && \
    rm -rf /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/corepack

# Install http-proxy for the proxy server (separate from app deps)
WORKDIR /srv
COPY server.js /srv/server.js
RUN bun init -y && bun install http-proxy && rm -rf /root/.bun/install/cache

# Pre-install common deps directly in /app (no copy needed at runtime)
WORKDIR /app
COPY package-template.json package.json
RUN bun install && \
    # Clean all caches (bun + any residual)
    rm -rf /root/.bun/install/cache /root/.npm /tmp/* && \
    # Strip junk from node_modules (docs, changelogs, tests, source maps)
    find node_modules \( -name 'README*' -o -name 'CHANGELOG*' -o -name 'HISTORY*' \
      -o -name 'LICENSE*' -o -name '*.md' -o -name '*.map' \
      -o -name '.eslintrc*' -o -name '.prettierrc*' -o -name 'tsconfig.json' \
      -o -name '.editorconfig' -o -name '.npmignore' \) -type f -delete 2>/dev/null; \
    find node_modules -type d \( -name '__tests__' -o -name 'test' -o -name 'tests' \
      -o -name 'docs' -o -name 'doc' -o -name 'example' -o -name 'examples' \) \
      -exec rm -rf {} + 2>/dev/null; true

# Create minimal app directory so Next.js can boot before files are synced
RUN mkdir -p app && \
    echo 'export default function RootLayout({ children }) { return <html><body>{children}</body></html> }' > app/layout.tsx && \
    echo 'export default function Page() { return <div style={{display:"flex",alignItems:"center",justifyContent:"center",height:"100vh",fontFamily:"system-ui"}}>Loading preview...</div> }' > app/page.tsx

# server.js now manages the full lifecycle: proxy, bun install, and Next.js
# It stops Next.js before bun install to prevent OOM, then restarts after.
EXPOSE 3000

CMD ["node", "/srv/server.js"]
