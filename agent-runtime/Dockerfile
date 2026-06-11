FROM langchain/langgraph-api:3.11

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends git curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /deps/agent-runtime

COPY pyproject.toml README.md langgraph.json ./
COPY wemob_agent ./wemob_agent

RUN PYTHONDONTWRITEBYTECODE=1 uv pip install --system --no-cache-dir -c /api/constraints.txt -e .

ENV LANGSERVE_GRAPHS='{"wemob_agent": "/deps/agent-runtime/wemob_agent/agent.py:create_agent"}' \
    LANGGRAPH_SERVER_PORT=2024 \
    UVICORN_TIMEOUT_KEEP_ALIVE=75

COPY start-agent-runtime.sh /app/start-agent-runtime.sh
RUN chmod +x /app/start-agent-runtime.sh \
    && mkdir -p /api/langgraph_api /api/langgraph_runtime /api/langgraph_license \
    && touch /api/langgraph_api/__init__.py /api/langgraph_runtime/__init__.py /api/langgraph_license/__init__.py \
    && PYTHONDONTWRITEBYTECODE=1 uv pip install --system --no-cache-dir --no-deps -e /api

EXPOSE 2024 2025

ENTRYPOINT ["/app/start-agent-runtime.sh"]
