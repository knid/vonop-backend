FROM node:20-alpine

WORKDIR /deps

# Pre-install common deps here (outside /app so volume mount doesn't overwrite)
COPY package-template.json package.json
RUN npm install

# Copy the proxy/sync server
COPY server.js /server.js

WORKDIR /app

EXPOSE 3000

# At startup: copy cached node_modules, install extras, start Next.js dev + proxy
CMD sh -c "\
  if [ ! -f /app/package.json ]; then cp /deps/package.json /app/package.json; fi; \
  cp -a /deps/node_modules /app/node_modules 2>/dev/null; \
  npm install --prefer-offline 2>&1; \
  ./node_modules/.bin/next dev --port 3001 --hostname 0.0.0.0 & \
  exec node /server.js"
