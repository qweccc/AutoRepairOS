# build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

ENV GOTOOLCHAIN=auto

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY locales/ ./locales/
COPY internal/ ./internal/
COPY web/ ./web/

RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

ENV TZ=Asia/Taipei

WORKDIR /app
COPY --from=builder /app/server ./server
COPY web/ ./web/

RUN mkdir -p /app/data

EXPOSE 8080

CMD ["./server"]
