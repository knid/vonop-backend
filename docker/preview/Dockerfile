FROM node:20-alpine

WORKDIR /deps

# Pre-install common deps here (outside /app so volume mount doesn't overwrite)
COPY package-template.json package.json
RUN npm install

# Install http-proxy for the proxy server (separate from app deps)
WORKDIR /srv
COPY server.js /srv/server.js
RUN npm init -y && npm install http-proxy

# Startup script: start proxy immediately, then install deps + Next.js in background
COPY --chmod=755 <<'STARTUP' /start.sh
#!/bin/sh
# Start the proxy server immediately (responds to health checks right away)
node /srv/server.js &

# Setup app deps in background
if [ ! -f /app/package.json ]; then cp /deps/package.json /app/package.json; fi
cp -a /deps/node_modules /app/node_modules 2>/dev/null
cd /app && npm install --prefer-offline 2>&1

# Start Next.js dev server
exec ./node_modules/.bin/next dev --port 3001 --hostname 0.0.0.0
STARTUP

WORKDIR /app

EXPOSE 3000

CMD ["/start.sh"]
