FROM golang:1.25.3 as builder

WORKDIR /workspace

ARG ORKA_VERSION=4.0.0
ARG TARGETARCH

# Make it runnable on a distroless image/without libc
ENV CGO_ENABLED=0

COPY go.mod go.sum ./

RUN go mod download

# Orka 4.0 renamed the CLI from orka3 to orka and moved its artifacts to the orka/ path.
# Download the build for the target architecture and install it as orka.
RUN set -eux \
    && major="${ORKA_VERSION%%.*}" \
    && if [ "$major" -ge 4 ]; then name=orka; else name=orka3; fi \
    && curl --location --fail --remote-name \
    "https://cli-builds-public.s3.eu-west-1.amazonaws.com/official/${ORKA_VERSION}/${name}/linux/${TARGETARCH:-amd64}/${name}.tar.gz" \
    && tar -xzf "${name}.tar.gz" -C /usr/local/bin \
    && if [ "$name" != orka ]; then mv "/usr/local/bin/${name}" /usr/local/bin/orka; fi

COPY . .

RUN make build

FROM alpine:latest

WORKDIR /

COPY --from=builder /workspace/bin/app /app
COPY --from=builder /usr/local/bin/orka /usr/local/bin/orka
# Keep orka3 working for custom scripts written for Orka 3.x.
RUN ln -s orka /usr/local/bin/orka3

RUN addgroup -S appgroup && adduser -S orka -G appgroup

USER orka

# We need to export this environment variable here to ensure that the runner can be automatically deployed into a Kubernetes cluster.
ENV KUBECONFIG=/home/orka/.kube/config

ENTRYPOINT ["/app"]
