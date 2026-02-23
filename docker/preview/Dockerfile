FROM node:20-alpine

# Install bun for fast package management
RUN npm install -g bun

# Install http-proxy for the proxy server (separate from app deps)
WORKDIR /srv
COPY server.js /srv/server.js
RUN npm init -y && bun install http-proxy

# Pre-install common deps directly in /app (no copy needed at runtime)
WORKDIR /app
COPY package-template.json package.json
RUN bun install

# Create minimal app directory so Next.js can boot before files are synced
RUN mkdir -p app && \
    echo 'export default function RootLayout({ children }) { return <html><body>{children}</body></html> }' > app/layout.tsx && \
    echo 'export default function Page() { return <div style={{display:"flex",alignItems:"center",justifyContent:"center",height:"100vh",fontFamily:"system-ui"}}>Loading preview...</div> }' > app/page.tsx

# Startup script: start proxy, install extra deps if needed, then start Next.js
COPY --chmod=755 <<'STARTUP' /start.sh
#!/bin/sh
# Start the proxy server immediately (responds to health checks right away)
node /srv/server.js &

# Deps are pre-installed in the image — mark setup complete immediately
touch /tmp/.setup-complete

# If sync already wrote a new package.json before we got here, install extra deps
# (must happen BEFORE Next.js starts so all deps are available for compilation)
if [ -f /tmp/.needs-bun-install ]; then
    rm -f /tmp/.needs-bun-install
    cd /app && bun install 2>&1
fi

# Start Next.js dev server (blocks — keeps container alive)
cd /app && exec ./node_modules/.bin/next dev --port 3001 --hostname 0.0.0.0
STARTUP

EXPOSE 3000

CMD ["/start.sh"]
