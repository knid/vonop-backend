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

# server.js now manages the full lifecycle: proxy, bun install, and Next.js
# It stops Next.js before bun install to prevent OOM, then restarts after.
EXPOSE 3000

CMD ["node", "/srv/server.js"]
