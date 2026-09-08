FROM golang:1.25.3 as builder

WORKDIR /workspace

# Android emulator support needs a CLI with the "orka3 emulator" command group, which this version
# provides. ORKA_EMULATOR_CONFIGS additionally needs "orka3 emulator-config", which it does not yet
# provide, so use ORKA_EMULATORS until that lands. Either way the runner fails at startup with a
# clear message when the CLI or cluster cannot serve what is configured.
ARG ORKA_VERSION=3.7.0-alpha

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
