FROM node:20-alpine

# Install bun for fast package management
RUN npm install -g bun

WORKDIR /deps

# Pre-install common deps here (outside /app so volume mount doesn't overwrite)
COPY package-template.json package.json
RUN bun install

# Install http-proxy for the proxy server (separate from app deps)
WORKDIR /srv
COPY server.js /srv/server.js
RUN npm init -y && bun install http-proxy

# Startup script: copy pre-built deps, start proxy + Next.js immediately
COPY --chmod=755 <<'STARTUP' /start.sh
#!/bin/sh
# Start the proxy server immediately (responds to health checks right away)
node /srv/server.js &

# Copy pre-built template deps (already installed at image build time)
if [ ! -f /app/package.json ]; then cp /deps/package.json /app/package.json; fi
cp -a /deps/node_modules /app/node_modules 2>/dev/null

# Mark setup complete — no install needed, deps are pre-built in the image.
# bun install only runs later if sync endpoint receives a changed package.json.
touch /tmp/.setup-complete

# If sync already wrote a new package.json before setup completed, install now
if [ -f /tmp/.needs-bun-install ]; then
    rm -f /tmp/.needs-bun-install
    cd /app && bun install 2>&1
fi

# Start Next.js dev server (blocks — keeps container alive)
cd /app && exec ./node_modules/.bin/next dev --port 3001 --hostname 0.0.0.0
STARTUP

WORKDIR /app

EXPOSE 3000

CMD ["/start.sh"]
