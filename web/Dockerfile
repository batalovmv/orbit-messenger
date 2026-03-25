FROM node:22-alpine AS builder

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts

RUN apk add --no-cache git
COPY . .
RUN git init && git checkout -b master 2>/dev/null || true
ARG SATURN_API_URL=http://localhost:8080/api/v1
ENV SATURN_API_URL=$SATURN_API_URL
ENV NODE_OPTIONS="--max-old-space-size=1024"
RUN npm run build:production

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
