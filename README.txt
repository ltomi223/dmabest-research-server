DMA Best EU FREE Manufacturer Research V6

No OpenAI API. No per-request AI token cost.

Flow:
Checker EXE -> Render backend -> direct official manufacturer pages + official web search -> official manual PDF text extraction -> pending profile -> cache

V6 changes:
- Direct official manufacturer URL candidates are tried before search-engine discovery.
- GIGABYTE product specification/support pages are checked for rev. 1.0 and rev. 1.1.
- Official PDF manuals discovered on manufacturer pages/search results are parsed with pdftotext.
- Only pages that actually yield relevant hardware/TPM data are recorded as sources.
- Unknown fields stay empty; no guessed TPM module.

Safety:
- Only official manufacturer domains are accepted as sources.
- Unknown fields stay empty.
- Automatic results are always PENDING until approved.
- Results are cached for 30 days.

Endpoints:
GET /health
POST /v1/research
