# syntax=docker/dockerfile:1.7
#
# wemob-base-nextjs base image.
#
# Pre-warms the npm tarball cache (/root/.npm) for the deps that show up in
# nearly every customer Next.js project. Customer Dockerfiles `FROM` this image
# and run `npm install --prefer-offline` to resolve cached packages locally
# instead of paying ~60–90s of registry round-trips on every cold build.
#
# Build + push manually (one-time per dep update):
#   flyctl apps create wemob-base-nextjs --org <wemob-org>   # only the first time
#   make base-image                                          # builds + pushes
#
# When a new dep enters internal/templates/nextjs/package.json or shows up in a
# significant fraction of agent-generated apps, add it to package.json here
# and rebuild.

FROM node:22-alpine
WORKDIR /seed
COPY package.json ./
# Populate /root/.npm with tarballs for every seed dep, then drop the resolved
# node_modules/ + package files so the final image stays close to ~250–350MB
# (vs ~700MB+ if we kept node_modules around). --ignore-scripts keeps the cache
# warming step deterministic and quick; customer builds rerun any postinstalls
# their own deps need against the warm cache. --legacy-peer-deps relaxes peer
# resolution so the seed install completes even when seeded packages have
# conflicting peer-dep ranges (e.g. better-auth wants drizzle-kit >=0.31.4
# while the canonical template pins drizzle-kit ^0.28.0); the resulting
# tarball cache is what we care about, not the resolved node_modules.
RUN npm install --no-audit --no-fund --ignore-scripts --legacy-peer-deps && \
    rm -rf /seed/node_modules /seed/package.json /seed/package-lock.json
WORKDIR /app
