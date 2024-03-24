FROM docker.io/library/golang:1.22.1-bookworm as builder

WORKDIR /app

RUN curl --proto '=https' --tlsv1.2 -fL -o ocb \
    https://github.com/open-telemetry/opentelemetry-collector/releases/download/cmd%2Fbuilder%2Fv0.96.0/ocb_0.96.0_linux_amd64 \
    && chmod +x ocb

COPY ./prometheusremotewritereceiver/ /app/prometheusremotewritereceiver/
COPY ./builder-config.yaml /app/builder-config.yaml

RUN ./ocb --config ./builder-config.yaml

FROM scratch

WORKDIR /app

COPY --from=builder /app/otelcol-prometheus-remote-write-receiver /app/otelcol-prometheus-remote-write-receiver

CMD ["/app/otelcol-prometheus-remote-write-receiver"]
