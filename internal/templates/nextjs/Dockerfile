# syntax=docker/dockerfile:1.7
# Multi-stage build for generated Next.js apps.
#
# Replaces Nixpacks to cut build-time memory (~2.5 GB → ~1 GB) and runtime
# image size (~500 MB → ~120 MB). Relies on next.config.mjs `output: "standalone"`.
#
# NEXT_PUBLIC_* vars are declared explicitly so Next.js can inline them into
# the client bundle at build time. The list mirrors integrations declared in
# internal/constants/integrations.go plus auto-provisioned e-commerce vars from
# internal/controllers/ecommerce_controller.go. When a new integration exposes
# a new NEXT_PUBLIC_* var, add matching ARG and ENV lines below.
#
# Coolify automatically injects all build-time env vars as --build-arg when
# using the dockerfile build pack, so we don't need to pass them explicitly.

FROM node:22-alpine AS deps
WORKDIR /app
# Lock file is optional — generated apps don't ship one, `npm install` resolves fresh.
COPY package.json package-lock.json* ./
RUN npm install --no-audit --no-fund --prefer-offline

FROM node:22-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# Ensure public/ exists even if the generated app didn't create one, so the
# runner stage COPY below never fails.
RUN mkdir -p ./public

# Public build-time env vars — add new NEXT_PUBLIC_* vars here when
# integrations.go or ecommerce_controller.go adds them.
ARG NEXT_PUBLIC_SUPABASE_URL
ARG NEXT_PUBLIC_SUPABASE_ANON_KEY
ARG NEXT_PUBLIC_FIREBASE_API_KEY
ARG NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN
ARG NEXT_PUBLIC_FIREBASE_PROJECT_ID
ARG NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET
ARG NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID
ARG NEXT_PUBLIC_FIREBASE_APP_ID
ARG NEXT_PUBLIC_STRIPE_PK
ARG NEXT_PUBLIC_CLOUDINARY_CLOUD_NAME
ARG NEXT_PUBLIC_CLOUDINARY_UPLOAD_PRESET
ARG NEXT_PUBLIC_GOOGLE_SHEET_ID
ARG NEXT_PUBLIC_STORE_ID
ARG NEXT_PUBLIC_WEMOB_API_URL
ARG NEXT_PUBLIC_WEMOB_GATEWAY_URL

ENV NEXT_PUBLIC_SUPABASE_URL=$NEXT_PUBLIC_SUPABASE_URL \
    NEXT_PUBLIC_SUPABASE_ANON_KEY=$NEXT_PUBLIC_SUPABASE_ANON_KEY \
    NEXT_PUBLIC_FIREBASE_API_KEY=$NEXT_PUBLIC_FIREBASE_API_KEY \
    NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN=$NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN \
    NEXT_PUBLIC_FIREBASE_PROJECT_ID=$NEXT_PUBLIC_FIREBASE_PROJECT_ID \
    NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET=$NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET \
    NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID=$NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID \
    NEXT_PUBLIC_FIREBASE_APP_ID=$NEXT_PUBLIC_FIREBASE_APP_ID \
    NEXT_PUBLIC_STRIPE_PK=$NEXT_PUBLIC_STRIPE_PK \
    NEXT_PUBLIC_CLOUDINARY_CLOUD_NAME=$NEXT_PUBLIC_CLOUDINARY_CLOUD_NAME \
    NEXT_PUBLIC_CLOUDINARY_UPLOAD_PRESET=$NEXT_PUBLIC_CLOUDINARY_UPLOAD_PRESET \
    NEXT_PUBLIC_GOOGLE_SHEET_ID=$NEXT_PUBLIC_GOOGLE_SHEET_ID \
    NEXT_PUBLIC_STORE_ID=$NEXT_PUBLIC_STORE_ID \
    NEXT_PUBLIC_WEMOB_API_URL=$NEXT_PUBLIC_WEMOB_API_URL \
    NEXT_PUBLIC_WEMOB_GATEWAY_URL=$NEXT_PUBLIC_WEMOB_GATEWAY_URL \
    NEXT_TELEMETRY_DISABLED=1

RUN npm run build

FROM node:22-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0

RUN addgroup -g 1001 -S nodejs && adduser -S nextjs -u 1001

COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public

USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
