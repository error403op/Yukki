############################################
#                BUILDER
############################################

FROM golang:1.25.5-bookworm AS builder

WORKDIR /build

# Install build tools (ADD tar + xz-utils → fixes extraction failures)
RUN apt-get update && \
    apt-get install -y \
        git \
        gcc \
        unzip \
        curl \
        zlib1g-dev \
        tar \
        xz-utils && \
    rm -rf /var/lib/apt/lists/*


# Better than go mod tidy for reproducible builds
COPY go.mod go.sum ./
RUN go mod download


COPY install.sh ./
COPY . .

RUN chmod +x install.sh && \
    ./install.sh -n --quiet --skip-summary && \
    CGO_ENABLED=1 go build -v -trimpath -ldflags="-w -s" -o app ./cmd/app/



############################################
#                RUNTIME
############################################

FROM debian:bookworm-slim

# Install runtime packages + aria2 (VERY IMPORTANT)
RUN apt-get update && \
    apt-get install -y \
        ffmpeg \
        curl \
        unzip \
        ca-certificates \
        nodejs \
        npm \
        zlib1g && \
    update-ca-certificates && \
    rm -rf /var/lib/apt/lists/*



#########################################################
# Networking fixes (helps Railway / weird DNS)
#########################################################

ENV GODEBUG=netdns=go+v4
ENV NTG_CALLS_IPV4_ONLY=1



#########################################################
# yt-dlp NIGHTLY (CRITICAL — NEVER USE STABLE)
#########################################################

RUN curl -L https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp \
    -o /usr/local/bin/yt-dlp && \
    chmod +x /usr/local/bin/yt-dlp



#########################################################
# Install Bun GLOBALLY (fixes root permission issue)
#########################################################

RUN curl -fsSL https://bun.sh/install | bash && \
    mv /root/.bun /usr/local/bun && \
    chmod -R 755 /usr/local/bun

ENV PATH="/usr/local/bun/bin:${PATH}"



#########################################################
# Install Deno WITHOUT installer script (stable method)
#########################################################

RUN curl -L https://github.com/denoland/deno/releases/latest/download/deno-x86_64-unknown-linux-gnu.zip \
    -o deno.zip && \
    unzip deno.zip && \
    mv deno /usr/local/bin/deno && \
    chmod +x /usr/local/bin/deno && \
    rm deno.zip



#########################################################
# VERIFY RUNTIMES (FAIL FAST — PRODUCTION RULE)
#########################################################

RUN node -v && \
    bun -v && \
    deno --version && \
    yt-dlp --version



#########################################################
# Performance / Stability ENV
#########################################################

ENV YTDLP_CACHE_DIR=/tmp/yt-cache
ENV PYTHONUNBUFFERED=1
ENV YTDLP_NO_PART_FILES=1
ENV GOMEMLIMIT=500MiB



#########################################################
# Certificates
#########################################################

COPY --from=builder /etc/ssl/certs /etc/ssl/certs



#########################################################
# Non-root user (security best practice)
#########################################################

RUN useradd -r -u 10001 appuser && \
    mkdir -p /app /tmp/yt-cache && \
    chown -R appuser:appuser /app /tmp/yt-cache

WORKDIR /app

COPY --from=builder /build/app /app/app
RUN chown appuser:appuser /app/app

USER appuser

ENTRYPOINT ["/app/app"]
