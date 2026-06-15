FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/infoblox-exporter /infoblox-exporter
USER nonroot:nonroot
EXPOSE 9717
ENTRYPOINT ["/infoblox-exporter"]
