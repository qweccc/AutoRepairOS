# build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY internal/ ./internal/
COPY migration/ ./migration/

RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /app/server ./server
COPY --from=builder /app/migration ./migration

RUN mkdir -p /app/data

EXPOSE 8080

CMD ["./server"]
