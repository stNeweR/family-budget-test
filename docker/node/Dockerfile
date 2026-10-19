# syntax=docker/dockerfile:1

# Development image: runs the Vite dev server (no static build).
# The compose service mounts ./frontend over /app for hot-reload.
FROM node:24-alpine
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm install
COPY frontend/ ./
EXPOSE 5173
CMD ["npm", "run", "dev"]
