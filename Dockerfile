FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG BUILD=none
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.build=${BUILD}" \
    -o /out/infoblox-exporter .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/infoblox-exporter /infoblox-exporter
USER nonroot:nonroot
EXPOSE 9717
ENTRYPOINT ["/infoblox-exporter"]
