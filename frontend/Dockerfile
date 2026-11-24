FROM node:22-alpine AS build
WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM nginx:1.27-alpine
# Dirender nginx image jadi /etc/nginx/conf.d/default.conf saat container start
# (envsubst, hanya variabel API_*). Ganti upstream API tanpa rebuild:
# env API_UPSTREAM=api:31101 di compose.
ENV API_UPSTREAM=api:8080
ENV NGINX_ENVSUBST_FILTER=^API_
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80
