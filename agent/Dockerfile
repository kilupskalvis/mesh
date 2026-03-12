FROM python:3.13-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    git curl && rm -rf /var/lib/apt/lists/*

# Install uv for fast, reproducible Python packaging.
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

WORKDIR /app
COPY pyproject.toml uv.lock ./
COPY src/ src/
RUN uv sync --frozen --no-dev

RUN useradd -m -s /bin/bash -u 1000 agent
ENV PYTHONUNBUFFERED=1 HOME=/home/agent
ENTRYPOINT ["uv", "run", "--frozen", "python", "-m", "mesh_agent"]
