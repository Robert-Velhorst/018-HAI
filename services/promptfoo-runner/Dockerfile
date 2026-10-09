# Keep the isolated runner aligned with the frontend's verified Node/npm toolchain.
FROM node:24.15.0-bookworm-slim

WORKDIR /app

COPY package.json package-lock.json ./

RUN --mount=type=cache,target=/root/.npm \
    npm install --global npm@10.9.8 --no-audit --no-fund \
    && test "$(npm --version)" = "10.9.8" \
    && npm ci --omit=dev --ignore-scripts --no-audit --no-fund

ENV PATH="/app/node_modules/.bin:${PATH}"

COPY app.mjs ./
COPY test_app.mjs ./

RUN node --test test_app.mjs \
    && PROMPTFOO_DISABLE_TELEMETRY=1 PROMPTFOO_DISABLE_UPDATE=1 promptfoo --version

RUN useradd --create-home --uid 10001 --shell /usr/sbin/nologin hai

ENV HOME=/tmp \
    XDG_CACHE_HOME=/tmp/cache \
    PROMPTFOO_DISABLE_TELEMETRY=true \
    PROMPTFOO_DISABLE_UPDATE=1 \
    NODE_ENV=production

EXPOSE 8080

USER 10001:10001

CMD ["node", "app.mjs"]
