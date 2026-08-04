FROM oven/bun:1.3.0

WORKDIR /app

COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

COPY tsconfig.json ./
COPY src ./src
RUN bun run build

FROM oven/bun:1.3.0

WORKDIR /app

COPY --from=0 /app/package.json /app/bun.lock ./
RUN bun install --frozen-lockfile --production
COPY --from=0 /app/dist ./dist

EXPOSE 8080
CMD ["bun", "dist/main.js"]
