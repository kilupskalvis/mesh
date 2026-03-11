FROM python:3.12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    git curl nodejs npm && rm -rf /var/lib/apt/lists/*

# gh CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update && apt-get install -y gh && rm -rf /var/lib/apt/lists/*

# glab CLI (GitLab) — optional, non-fatal if download URL changes
RUN curl -fsSL "https://gitlab.com/gitlab-org/cli/-/releases/permalink/latest/downloads/glab_$(dpkg --print-architecture).deb" -o /tmp/glab.deb \
    && dpkg -i /tmp/glab.deb && rm /tmp/glab.deb || true

# LSP server binaries
RUN pip install --no-cache-dir pyright
RUN npm install -g typescript typescript-language-server @anthropic-ai/claude-code

# LSP plugin configs from official marketplace (optional, repo layout may change)
RUN git clone --depth 1 https://github.com/anthropics/claude-plugins-official.git /tmp/plugins \
    && mkdir -p /app/plugins \
    && (cp -r /tmp/plugins/pyright-lsp /app/plugins/pyright-lsp 2>/dev/null || true) \
    && (cp -r /tmp/plugins/typescript-lsp /app/plugins/typescript-lsp 2>/dev/null || true) \
    && rm -rf /tmp/plugins

WORKDIR /app
COPY pyproject.toml .
COPY src/ src/
COPY prompts/ prompts/
RUN pip install --no-cache-dir .

RUN useradd -m -s /bin/bash -u 1000 agent
ENV PYTHONUNBUFFERED=1 HOME=/home/agent
ENTRYPOINT ["python", "-m", "mesh_agent"]
