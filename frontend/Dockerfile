# Angular 22.2.2 supports Node 26.0.0+. Pin current stable.
# Avoid floating Alpine tags that can move builds to an unsupported release.
FROM node:26.12.1-alpine@sha256:40cd011b55415bc894e45b02fdd5a5dfde056921b7d23a64c74f80497af48797 AS build
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
FROM nginx:1.30.6-alpine-slim@sha256:3028fc1419f99b95c1c69f844bbaf49dc1b881750c51b497fc721ac4f02a14e6
COPY --from=build /app/dist/app /usr/share/nginx/html

COPY ./nginx-custom.conf /etc/nginx/conf.d/default.conf
