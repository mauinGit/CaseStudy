# ---- Build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache dependencies separately from source code.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# ---- Runtime stage ----
# distroless/static: no shell, no package manager, CA certs + tzdata included, runs as non-root.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /app/api

ENV APP_ENV=production \
    PORT=8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
