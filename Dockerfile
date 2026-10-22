# syntax=docker/dockerfile:1
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY api /src/api
COPY web ./
# vite writes to ../internal/web/static/dist (embedded by Go)
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/static/dist ./internal/web/static/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tierl ./cmd/tierl

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tierl /tierl
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/tierl"]
