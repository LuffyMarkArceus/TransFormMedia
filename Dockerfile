FROM golang:1.25-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o /app/server ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ffmpeg ca-certificates tzdata wget

RUN adduser -D -u 1001 appuser

COPY --from=build /app/server /app/server

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

USER appuser

EXPOSE 8080

CMD ["/app/server"]
