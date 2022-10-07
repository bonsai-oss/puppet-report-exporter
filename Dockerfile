FROM golang:alpine AS builder
WORKDIR /build
COPY . /build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -trimpath -o /build/puppet-report-exporter cmd/puppet-report-exporter/*.go
RUN apk add -U --no-cache ca-certificates

FROM scratch
EXPOSE 9115
COPY --from=builder /build/puppet-report-exporter /app/puppet-report-exporter
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["/app/puppet-report-exporter"]
