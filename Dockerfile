FROM golang:1.26 AS build-stage

WORKDIR /app

# Download modules in their own layer, so source changes don't throw away the module cache
COPY go.mod go.sum ./
RUN go mod download

COPY . ./

# cgo is needed for sqlite. netgo and osusergo use Go's own DNS and user lookups, which glibc
# can't provide in a statically linked binary.
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -tags netgo,osusergo -o /server -ldflags '-linkmode external -extldflags "-static"' .

# Run the tests in the container
FROM build-stage AS run-test-stage
RUN go test ./...

# Deploy the application binary into a lean image
FROM alpine:3.22 AS build-release-stage

WORKDIR /

RUN apk --no-cache add ca-certificates

# Copied from the test stage, so the image is only built once the tests pass
COPY --from=run-test-stage /server /server

EXPOSE 8080

# busybox wget is part of alpine. PORT is the same variable the server reads.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -q -O /dev/null "http://127.0.0.1:${PORT:-8080}/healthz" || exit 1

ENTRYPOINT ["/server"]
