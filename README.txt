DMA Best EU HYBRID Research V7

NO OPENAI API. NO PER-REQUEST AI TOKEN COST.

Priority:
1. Built-in VERIFIED motherboard database (motherboards.json)
2. If no verified match: official manufacturer pages/manuals
3. If still uncertain: fields remain empty and result stays PENDING

This gives known boards deterministic, repeatable results while still allowing
new/unknown boards to fall back to official web research.

Current verified seed profiles:
- GIGABYTE X870E AORUS PRO ICE (generic revision profile + Rev. 1.1 exact profile)
- MSI X570-A PRO header/chipset/socket profile

To expand the verified database later:
edit motherboards.json in GitHub and deploy the latest commit.
The customer EXE does NOT need to be replaced.

Endpoints:
GET /health
POST /v1/research
