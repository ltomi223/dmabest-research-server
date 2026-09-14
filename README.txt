DMA Best EU V33 - REAL HARDWARE NORMALIZATION HOTFIX

GLOBAL MATCHING FIX FOR REAL WINDOWS/SMBIOS STRINGS.

Example fixed:
Micro-Star International Co., Ltd.
B760 GAMING PLUS WIFI (MS-7D98)
Rev 3.0

now matches the existing VERIFIED database profile:
MSI
B760 GAMING PLUS WIFI
Revision *

Global normalization:
- Micro-Star International Co., Ltd. -> MSI
- ASUSTeK COMPUTER INC. -> ASUS
- GIGABYTE variants -> GIGABYTE
- ASRock variants -> ASROCK
- strips trailing MSI board IDs like (MS-7D98) and [MS-7D98]
- DOES NOT merge DDR4/non-DDR4 commercial model names
- revision logic stays active

Database count remains 987 VERIFIED profiles.
This is server-side: existing user/friend checker EXE can stay unchanged.
