FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /app/elasticsearch-vm-autoscaler cmd/main.go

FROM alpine:3.18
RUN apk --no-cache add ca-certificates bash
WORKDIR /app
COPY --from=builder /app/elasticsearch-vm-autoscaler /app/
CMD ["./elasticsearch-vm-autoscaler"]
