# Angular 22 requires Node 22.22.3 or newer. Both stages are pinned by digest
# so a rebuild cannot silently change either toolchain or runtime.
FROM node:22.22.3-alpine@sha256:e58326d0d441090181ac150dc2078d3e2cf6a0d42e809aebba3ef5880935ffdd AS build

WORKDIR /app

COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --prefer-offline --no-audit --no-fund

COPY . .
RUN npm run build

FROM nginx:stable-alpine-slim@sha256:ddde39c6e51f02fde7410c2e9c234cf2d0a4c7bdbbe176aeb37d8ad7ab4eb58c

COPY --from=build /app/dist/app /usr/share/nginx/html
COPY ./nginx-main.conf /etc/nginx/nginx.conf
COPY ./nginx-custom.conf /etc/nginx/conf.d/default.conf

USER 101:101

ENTRYPOINT ["nginx", "-g", "daemon off;"]
