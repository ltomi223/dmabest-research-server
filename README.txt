DMA Best EU Research V14 - CACHE + DATABASE FIX

Database version: V14
Canonical profiles: 35
Removed duplicate generic MSI records: 6

Main fixes:
1. VERIFIED database lookup now runs BEFORE cache.
   Old PENDING cache entries can no longer hide new VERIFIED profiles.
2. Exact revision match is preferred over wildcard profiles.
3. Duplicate generic MSI profiles were consolidated.
4. /health now shows databaseVersion, databaseSchema, verifiedProfiles and generated date.

After deployment, open:
https://dmabest-research-server.onrender.com/health

Expected:
databaseVersion = V14
verifiedProfiles = 35
