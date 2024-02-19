FROM golang:1.21.6 as builder

WORKDIR /workspace

ARG ORKA_VERSION=3.0.1

# Make it runnable on a distroless image/without libc
ENV CGO_ENABLED=0

COPY go.mod go.sum ./

RUN go mod download

RUN set -eux \
    && curl --location --fail --remote-name \
    https://cli-builds-public.s3.eu-west-1.amazonaws.com/official/${ORKA_VERSION}/orka3/linux/amd64/orka3.tar.gz \
    && tar -xzf orka3.tar.gz -C /usr/local/bin

COPY . .

RUN make build

FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/bin/app /app

ENTRYPOINT ["/app"]
