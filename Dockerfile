FROM golang:1.23.0 as builder

WORKDIR /workspace

ARG ORKA_VERSION=3.1.0

ARG TARGETARCH=amd64

# Make it runnable on a distroless image/without libc
ENV CGO_ENABLED=0

COPY go.mod go.sum ./

RUN go mod download

# Download Orka CLI for the specified architecture
# For Intel-based Orka nodes: use amd64 (default)
# For Apple Silicon-based Orka nodes: use arm64

RUN set -eux \
    && curl --location --fail --remote-name \
    https://cli-builds-public.s3.eu-west-1.amazonaws.com/official/${ORKA_VERSION}/orka3/linux/${TARGETARCH}/orka3.tar.gz \
    && tar -xzf orka3.tar.gz -C /usr/local/bin

COPY . .

RUN make build

FROM alpine:latest

WORKDIR /

COPY --from=builder /workspace/bin/app /app
COPY --from=builder /usr/local/bin/orka3 /usr/local/bin/orka3

RUN addgroup -S appgroup && adduser -S orka -G appgroup

USER orka

# We need to export this environment variable here to ensure that the runner can be automatically deployed into a Kubernetes cluster.
ENV KUBECONFIG=/home/orka/.kube/config

ENTRYPOINT ["/app"]
