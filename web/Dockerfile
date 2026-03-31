FROM node:22-alpine AS builder

WORKDIR /app
COPY package.json package-lock.json ./
RUN apk add --no-cache git bash
RUN npm ci --ignore-scripts
COPY . .
RUN git init && git add -A && git -c user.name=build -c user.email=build@ci commit -m "build" --quiet 2>/dev/null || true
ENV NODE_OPTIONS="--max-old-space-size=1024"
RUN npm run build:production && \
    cd dist && ls -la rlottie* || true && \
    actual=$(ls rlottie-wasm.*.js 2>/dev/null | head -1) && \
    if [ -n "$actual" ]; then \
      grep -roh 'rlottie-wasm\.[a-f0-9]\{16,\}\.js' *.js 2>/dev/null | sort -u | while read ref; do \
        [ ! -f "$ref" ] && [ "$ref" != "$actual" ] && cp "$actual" "$ref" && echo "Patched: $actual -> $ref"; \
      done; true; \
    fi

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/templates/default.conf.template
EXPOSE 80
