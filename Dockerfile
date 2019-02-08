FROM golang:alpine AS builder

COPY . /go/src/github.com/govau/cga-log-viewer

# If we don't disable CGO, the binary won't work in the scratch image. Unsure why?
RUN CGO_ENABLED=0 go install github.com/govau/cga-log-viewer

FROM scratch

COPY --from=builder /go/bin/cga-log-viewer /go/bin/cga-log-viewer
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ENTRYPOINT ["/go/bin/cga-log-viewer"]
