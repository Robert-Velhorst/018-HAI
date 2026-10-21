# Angular 22.2.2 supports Node 26.0.0+. Pin current stable.
# Avoid floating Alpine tags that can move builds to an unsupported release.
FROM node:26.12.0-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./

RUN --mount=type=cache,target=/root/.npm \
    npm install --global npm@12.2.1 --no-audit --no-fund \
    && test "$(npm --version)" = "12.2.1" \
    && npm ci --prefer-offline --no-audit --no-fund


COPY . .
ARG HAI_FRONTEND_BUILD_WORKERS=2
RUN case "$HAI_FRONTEND_BUILD_WORKERS" in \
        1|2|4|8) ;; \
        *) echo "HAI_FRONTEND_BUILD_WORKERS must be one of 1, 2, 4, 8" >&2; exit 1 ;; \
    esac \
    && NG_BUILD_MAX_WORKERS="$HAI_FRONTEND_BUILD_WORKERS" npm run build

# Stage 2: Serve the app with Nginx
FROM nginx:1.31.7-alpine-slim@sha256:ebd058041a732a2b2dc0b606e34572dd8e0034de6a4aa4d50d8c13a240ac6005
COPY --from=build /app/dist/app /usr/share/nginx/html

COPY ./nginx-custom.conf /etc/nginx/conf.d/default.conf
