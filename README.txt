DMA Best EU Central Research V4

FINAL ARCHITECTURE
------------------
Customer PC:
  DMA Best EU Checker.exe
       |
       | HTTPS
       v
  https://research.dmabest.eu/v1/research
       |
       v
  Central Research Server
       |
       v
  OpenAI Responses API + web_search
       |
       v
  PENDING compatibility profile

The OpenAI API key exists ONLY on the server.
It is never shipped inside the customer EXE.

Required server environment variables:
  OPENAI_API_KEY
  DMABEST_MODEL (optional, default gpt-6-astra)

Health endpoint:
  GET /health

Research endpoint:
  POST /v1/research

Recommended deployment:
  Docker-capable service (Render, Railway, VPS, Fly.io, etc.)
  Then point research.dmabest.eu to the deployed server.

Safety rule:
  Automatic research always returns status=pending.
  Human approval is required before a profile becomes verified.
