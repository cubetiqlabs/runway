FROM ghcr.io/actions/actions-runner:latest

LABEL org.opencontainers.image.source="https://github.com/cubetiqlabs/runway" \
      org.opencontainers.image.description="Batteries-included GitHub Actions runner for Runway (Docker CLI, Compose v2, Buildx, Node.js 24, make, build-essential, gh, jq)" \
      org.opencontainers.image.licenses="MIT"

USER root

ENV DEBIAN_FRONTEND=noninteractive \
    CI=true \
    RUNNER_TOOL_CACHE=/opt/hostedtoolcache \
    AGENT_TOOLSDIRECTORY=/opt/hostedtoolcache

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      gnupg \
      lsb-release \
      build-essential \
      pkg-config \
      git \
      git-lfs \
      jq \
      zip \
      unzip \
      tar \
      gzip \
      xz-utils \
      zstd \
      rsync \
      openssh-client \
      python3 \
      python3-pip \
      wget \
      file \
      patch; \
    install -m 0755 -d /etc/apt/keyrings; \
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg; \
    chmod a+r /etc/apt/keyrings/docker.gpg; \
    . /etc/os-release; \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable" > /etc/apt/sources.list.d/docker.list; \
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg; \
    chmod a+r /etc/apt/keyrings/githubcli-archive-keyring.gpg; \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list; \
    curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg; \
    chmod a+r /etc/apt/keyrings/nodesource.gpg; \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_24.x nodistro main" > /etc/apt/sources.list.d/nodesource.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      docker-ce-cli \
      docker-buildx-plugin \
      docker-compose-plugin \
      gh \
      nodejs; \
    mkdir -p /opt/hostedtoolcache; \
    chown -R runner:runner /opt/hostedtoolcache; \
    git lfs install --system; \
    make --version; \
    node --version; \
    npm --version; \
    docker --version; \
    docker compose version; \
    docker buildx version; \
    gh --version; \
    jq --version; \
    rm -rf /var/lib/apt/lists/*

USER runner
WORKDIR /home/runner
