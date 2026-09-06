FROM golang:1.26-alpine AS build

WORKDIR /src
COPY main.go .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dns-loadgen main.go

FROM scratch
COPY --from=build --chown=65532:65532 /out/dns-loadgen /dns-loadgen
USER 65532:65532
ENTRYPOINT ["/dns-loadgen"]
