FROM node:20-alpine AS builder
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN npm install -g pnpm

RUN pnpm install --frozen-lockfile
COPY . .
ENV NODE_ENV production

RUN pnpm build

FROM node:20-alpine AS runner
WORKDIR /app
COPY --from=builder /app/next.config.ts ./
COPY --from=builder /app/public ./public
COPY --from=builder /app/.next ./.next
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package.json ./package.json
RUN npm install -g pnpm

EXPOSE 3000
CMD ["pnpm", "start"]