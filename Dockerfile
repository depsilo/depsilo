# Stage 1: Build frontend
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build backend
FROM golang:1.21-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN go build -o repocache ./cmd/server

# Stage 3: Final image
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=backend /app/repocache .
EXPOSE 23333
CMD ["./repocache"]
