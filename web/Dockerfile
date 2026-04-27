FROM node:22-alpine AS builder

WORKDIR /app
COPY package.json package-lock.json ./
RUN apk add --no-cache git bash
RUN npm ci --ignore-scripts
COPY . .
RUN git init && git add -A && git -c user.name=build -c user.email=build@ci commit -m "build" --quiet 2>/dev/null || true
ENV NODE_OPTIONS="--max-old-space-size=1024"
ARG VAPID_PUBLIC_KEY=""
ENV VAPID_PUBLIC_KEY=${VAPID_PUBLIC_KEY}
# Gate build on tsc + stylelint + eslint — Saturn deploy is blocked if checks fail.
RUN npm run check
# Force APP_ENV=production inline — Saturn platform may inject its own APP_ENV
# as a build-time env var, overriding Docker ENV. Inline assignment cannot be overridden.
RUN APP_ENV=production npm run build:production

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY nginx-snippets /etc/nginx/snippets
# Default to 80; Saturn overrides PORT at runtime
ENV PORT=80
EXPOSE 80
