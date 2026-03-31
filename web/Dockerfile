FROM node:22-alpine AS builder

WORKDIR /app
COPY package.json package-lock.json ./
RUN apk add --no-cache git bash
RUN npm ci --ignore-scripts
COPY . .
RUN git init && git add -A && git -c user.name=build -c user.email=build@ci commit -m "build" --quiet 2>/dev/null || true
ENV NODE_OPTIONS="--max-old-space-size=1024"
RUN npm run build:production && \
    echo "=== rlottie files in dist ===" && \
    ls -la dist/rlottie* && \
    echo "=== 6805 chunk ===" && \
    ls dist/6805* && \
    echo "=== rlottie reference in 6805 ===" && \
    grep -o 'rlottie-wasm[^"]*\.js' dist/6805*.js || echo "no rlottie ref found"

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/templates/default.conf.template
EXPOSE 80
