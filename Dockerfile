FROM golang:1.25.3 as builder

WORKDIR /workspace

# Android emulator support (ORKA_EMULATOR_CONFIGS) needs a CLI that provides the
# "orka3 emulator" and "orka3 emulator-config" command groups. Bump this pin before enabling it;
# the runner fails at startup with a clear message if the CLI or cluster cannot serve emulators.
ARG ORKA_VERSION=3.1.0

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

FROM alpine:latest

WORKDIR /

COPY --from=builder /workspace/bin/app /app
COPY --from=builder /usr/local/bin/orka3 /usr/local/bin/orka3

RUN addgroup -S appgroup && adduser -S orka -G appgroup

USER orka

# We need to export this environment variable here to ensure that the runner can be automatically deployed into a Kubernetes cluster.
ENV KUBECONFIG=/home/orka/.kube/config

ENTRYPOINT ["/app"]
