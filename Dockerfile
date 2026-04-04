FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o deeplink ./cmd/deeplink

FROM alpine:3.21
RUN apk --no-cache add ca-certificates
WORKDIR /app

COPY --from=builder /app/templates/default /app/templates/default
COPY --from=builder /app/deeplink /app/deeplink
CMD ["./deeplink"]
