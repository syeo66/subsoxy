FROM golang:1.24 AS build-stage

WORKDIR /app

COPY . ./
RUN go mod download

RUN CGO_ENABLED=1 GOOS=linux go build -o /server -a -ldflags '-linkmode external -extldflags "-static"' .

# Run the tests in the container
FROM build-stage AS run-test-stage
RUN go test -v ./...

# Deploy the application binary into a lean image
FROM alpine AS build-release-stage

WORKDIR /

RUN apk --no-cache add --no-check-certificate ca-certificates \
  && update-ca-certificates

COPY --from=build-stage /server /server

EXPOSE 3333

ENTRYPOINT ["/server"]
