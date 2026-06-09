FROM python:3.12-slim

LABEL org.opencontainers.image.source="https://github.com/knid/wemob-backend" \
      org.opencontainers.image.description="Wemob DeepAgents runtime" \
      org.opencontainers.image.role="deepagents-runtime"

RUN groupadd -r runtime && useradd -r -g runtime -d /app -s /sbin/nologin runtime

WORKDIR /app

COPY pyproject.toml ./

RUN pip install --no-cache-dir --upgrade pip \
    && pip install --no-cache-dir fastapi uvicorn pydantic deepagents langchain langgraph langchain-anthropic langchain-openai

COPY app ./app
RUN pip install --no-cache-dir -e . \
    && chown -R runtime:runtime /app

USER runtime

EXPOSE 8000

HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8000/healthz')" || exit 1

CMD ["uvicorn", "app.main:create_app", "--factory", "--host", "0.0.0.0", "--port", "8000"]
