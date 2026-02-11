FROM node:20-alpine

WORKDIR /deps

# Pre-install common deps here (outside /app so volume mount doesn't overwrite)
COPY package-template.json package.json
RUN npm install

WORKDIR /app

EXPOSE 3000

# At startup: copy cached node_modules, preserve project's package.json, install extras, start
CMD sh -c "\
  if [ ! -f /app/package.json ]; then cp /deps/package.json /app/package.json; fi; \
  cp -a /deps/node_modules /app/node_modules 2>/dev/null; \
  npm install --prefer-offline 2>&1; \
  exec ./node_modules/.bin/next dev --port 3000 --hostname 0.0.0.0"
