FROM golang:1.25 AS builder
WORKDIR /build
COPY main.go .
RUN CGO_ENABLED=0 go build -o mock-hyperfleet-api main.go

FROM scratch
COPY --from=builder /build/mock-hyperfleet-api /mock-hyperfleet-api
ENTRYPOINT ["/mock-hyperfleet-api"]

