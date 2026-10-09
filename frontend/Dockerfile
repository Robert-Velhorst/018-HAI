# Angular 22.1.1 supports Node 22.22.3+ or 24.15.0+. Pin the active LTS
# toolchain so a floating Alpine tag cannot move builds to an unsupported release.
FROM node:24.15.0-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./

RUN --mount=type=cache,target=/root/.npm \
    npm install --global npm@10.9.8 --no-audit --no-fund \
    && test "$(npm --version)" = "10.9.8" \
    && npm ci --prefer-offline --no-audit --no-fund


COPY . .
ARG HAI_FRONTEND_BUILD_WORKERS=2
RUN case "$HAI_FRONTEND_BUILD_WORKERS" in \
        1|2|4|8) ;; \
        *) echo "HAI_FRONTEND_BUILD_WORKERS must be one of 1, 2, 4, 8" >&2; exit 1 ;; \
    esac \
    && NG_BUILD_MAX_WORKERS="$HAI_FRONTEND_BUILD_WORKERS" npm run build

# Stage 2: Serve the app with Nginx
FROM nginx:stable-alpine-slim
COPY --from=build /app/dist/app /usr/share/nginx/html

COPY ./nginx-custom.conf /etc/nginx/conf.d/default.conf
