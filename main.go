package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Hardware struct {
	Manufacturer       string `json:"manufacturer"`
	Product            string `json:"product"`
	Version            string `json:"version"`
	CPU                string `json:"cpu"`
	BIOS               string `json:"bios"`
	TPMPresent         *bool  `json:"tpmPresent"`
	TPMReady           *bool  `json:"tpmReady"`
	TPMSpec            string `json:"tpmSpec"`
	SecureBoot         *bool  `json:"secureBoot"`
	SystemManufacturer string `json:"systemManufacturer"`
	SystemModel        string `json:"systemModel"`
}

type ResearchResult struct {
	Status       string   `json:"status"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	Chipset      string   `json:"chipset,omitempty"`
	Socket       string   `json:"socket,omitempty"`
	Header       string   `json:"header,omitempty"`
	Bus          string   `json:"bus,omitempty"`
	PinLayout    string   `json:"pinLayout,omitempty"`
	Module       string   `json:"module,omitempty"`
	BIOSHint     string   `json:"biosHint,omitempty"`
	Confidence   int      `json:"confidence"`
	Sources      []string `json:"sources"`
	Evidence     []string `json:"evidence"`
	Missing      []string `json:"missing"`
	Note         string   `json:"note"`
	Updated      string   `json:"updated"`
	Cached       bool     `json:"cached,omitempty"`
}

type VerifiedProfile struct {
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	Revision     string   `json:"revision"`
	Status       string   `json:"status"`
	Chipset      string   `json:"chipset"`
	Socket       string   `json:"socket"`
	Header       string   `json:"header"`
	Bus          string   `json:"bus"`
	PinLayout    string   `json:"pinLayout"`
	Module       string   `json:"module"`
	Confidence   int      `json:"confidence"`
	Note         string   `json:"note"`
	Sources      []string `json:"sources"`
	Evidence     []string `json:"evidence"`
}

type VerifiedDatabase struct {
	SchemaVersion int               `json:"schemaVersion"`
	Generated     string            `json:"generated"`
	Profiles      []VerifiedProfile `json:"profiles"`
}

//go:embed motherboards.json
var motherboardDBJSON []byte

var (
	dbOnce sync.Once
	dbData VerifiedDatabase
)

func verifiedDB() VerifiedDatabase {
	dbOnce.Do(func() {
		if err := json.Unmarshal(motherboardDBJSON, &dbData); err != nil {
			log.Printf("motherboard database parse error: %v", err)
		} else {
			log.Printf("verified motherboard DB loaded: %d profiles", len(dbData.Profiles))
		}
	})
	return dbData
}

func normDBText(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, " TECHNOLOGY CO., LTD.", "")
	v = strings.ReplaceAll(v, " TECHNOLOGY CO., LTD", "")
	v = strings.ReplaceAll(v, " INC.", "")
	v = strings.ReplaceAll(v, " INC", "")
	return strings.Join(strings.Fields(v), " ")
}

func lookupVerifiedProfile(h Hardware) (ResearchResult, bool) {
	db := verifiedDB()
	maker := normDBText(h.Manufacturer)
	if maker == "" {
		maker = normDBText(h.SystemManufacturer)
	}
	model := normDBText(h.Product)
	if model == "" {
		model = normDBText(h.SystemModel)
	}
	rev := strings.TrimSpace(h.Version)
	revUpper := strings.ToUpper(rev)
	for _, prefix := range []string{"REVISION ", "REVISION", "REV. ", "REV.", "REV ", "REV"} {
		if strings.HasPrefix(revUpper, prefix) {
			rev = strings.TrimSpace(rev[len(prefix):])
			break
		}
	}

	var candidates []VerifiedProfile
	for _, p := range db.Profiles {
		pm := normDBText(p.Manufacturer)
		pp := normDBText(p.Model)
		if pp != model {
			continue
		}
		if pm != "" && maker != "" && !strings.Contains(maker, pm) && !strings.Contains(pm, maker) {
			continue
		}
		candidates = append(candidates, p)
	}

	matchRevision := func(p VerifiedProfile, allowWildcard bool) bool {
		pr := strings.TrimSpace(p.Revision)
		if allowWildcard && pr == "*" {
			return true
		}
		if strings.EqualFold(pr, rev) {
			return true
		}
		if strings.Contains(pr, "/") {
			for _, part := range strings.Split(pr, "/") {
				if strings.EqualFold(strings.TrimSpace(part), rev) {
					return true
				}
			}
		}
		if strings.EqualFold(pr, "1.x") && strings.HasPrefix(strings.ToLower(rev), "1.") {
			return true
		}
		return false
	}

	makeResult := func(p VerifiedProfile) ResearchResult {
		r := ResearchResult{
			Status:       p.Status,
			Manufacturer: p.Manufacturer,
			Model:        p.Model,
			Chipset:      p.Chipset,
			Socket:       p.Socket,
			Header:       p.Header,
			Bus:          p.Bus,
			PinLayout:    p.PinLayout,
			Module:       p.Module,
			Confidence:   p.Confidence,
			Sources:      p.Sources,
			Evidence:     p.Evidence,
			Note:         p.Note,
			Updated:      time.Now().Format(time.RFC3339),
		}
		missingFields(&r)
		return r
	}

	// Exact/grouped revision first.
	for _, p := range candidates {
		if strings.TrimSpace(p.Revision) != "*" && matchRevision(p, false) {
			return makeResult(p), true
		}
	}
	// Generic wildcard profile second.
	for _, p := range candidates {
		if strings.TrimSpace(p.Revision) == "*" && matchRevision(p, true) {
			return makeResult(p), true
		}
	}

	// Multiple revision-specific profiles exist but revision is unresolved.
	if len(candidates) > 1 {
		base := candidates[0]
		r := ResearchResult{
			Status:       "pending",
			Manufacturer: base.Manufacturer,
			Model:        base.Model,
			Chipset:      base.Chipset,
			Socket:       base.Socket,
			Header:       base.Header,
			Bus:          base.Bus,
			PinLayout:    base.PinLayout,
			Module:       "Revision required",
			Confidence:   95,
			Sources:      base.Sources,
			Evidence:     base.Evidence,
			Note:         "Az alaplap megtalálható az adatbázisban, de a pontos PCB revízió szükséges a TPM modul kiválasztásához.",
			Updated:      time.Now().Format(time.RFC3339),
		}
		missingFields(&r)
		return r, true
	}
	return ResearchResult{}, false
}

type Config struct {
	Listen   string
	CacheDir string
}

var cacheMu sync.Mutex

func loadConfig() Config {
	c := Config{Listen: "0.0.0.0:8787", CacheDir: "data/cache"}
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		c.Listen = "0.0.0.0:" + p
	} else if v := strings.TrimSpace(os.Getenv("DMABEST_LISTEN")); v != "" {
		c.Listen = v
	}
	if v := strings.TrimSpace(os.Getenv("DMABEST_CACHE_DIR")); v != "" {
		c.CacheDir = v
	}
	return c
}

func normMaker(s string) string {
	u := strings.ToUpper(s)
	switch {
	case strings.Contains(u, "GIGABYTE"):
		return "GIGABYTE"
	case strings.Contains(u, "MICRO-STAR") || regexp.MustCompile(`\bMSI\b`).MatchString(u):
		return "MSI"
	case strings.Contains(u, "ASUSTEK") || regexp.MustCompile(`\bASUS\b`).MatchString(u):
		return "ASUS"
	case strings.Contains(u, "ASROCK"):
		return "ASROCK"
	case strings.Contains(u, "BIOSTAR"):
		return "BIOSTAR"
	case strings.Contains(u, "DELL"):
		return "DELL"
	case strings.Contains(u, "LENOVO"):
		return "LENOVO"
	case strings.Contains(u, "HEWLETT") || regexp.MustCompile(`\bHP\b`).MatchString(u):
		return "HP"
	case strings.Contains(u, "CLEVO"):
		return "CLEVO"
	case strings.Contains(u, "XMG"):
		return "XMG"
	}
	return strings.TrimSpace(s)
}

func officialDomain(maker string) string {
	switch maker {
	case "GIGABYTE":
		return "gigabyte.com"
	case "MSI":
		return "msi.com"
	case "ASUS":
		return "asus.com"
	case "ASROCK":
		return "asrock.com"
	case "BIOSTAR":
		return "biostar.com.tw"
	case "DELL":
		return "dell.com"
	case "LENOVO":
		return "lenovo.com"
	case "HP":
		return "hp.com"
	case "CLEVO":
		return "clevo.com.tw"
	case "XMG":
		return "xmg.gg"
	}
	return ""
}

func cacheKey(h Hardware) string {
	raw := strings.ToUpper(strings.Join([]string{
		h.Manufacturer, h.Product, h.Version, h.SystemManufacturer, h.SystemModel,
	}, "|"))
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}
func cachePath(cfg Config, h Hardware) string {
	return filepath.Join(cfg.CacheDir, cacheKey(h)+".json")
}

func loadCache(cfg Config, h Hardware) (ResearchResult, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	b, err := os.ReadFile(cachePath(cfg, h))
	if err != nil {
		return ResearchResult{}, false
	}
	var r ResearchResult
	if json.Unmarshal(b, &r) != nil {
		return ResearchResult{}, false
	}
	if r.Updated != "" {
		if t, err := time.Parse(time.RFC3339, r.Updated); err == nil && time.Since(t) > 30*24*time.Hour {
			return ResearchResult{}, false
		}
	}
	r.Cached = true
	return r, true
}
func saveCache(cfg Config, h Hardware, r ResearchResult) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	_ = os.MkdirAll(cfg.CacheDir, 0755)
	r.Cached = false
	b, _ := json.MarshalIndent(r, "", "  ")
	_ = os.WriteFile(cachePath(cfg, h), b, 0644)
}

func httpClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 6 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func fetch(rawURL string) (string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; DMA-Best-EU-Checker/1.0)")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func stripHTML(s string) string {
	s = regexp.MustCompile(`(?is)<script.*?</script>`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`(?is)<style.*?</style>`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func searchOfficial(maker, model string) []string {
	domain := officialDomain(maker)
	if domain == "" {
		return nil
	}
	q := fmt.Sprintf(`site:%s "%s" TPM`, domain, model)
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(q)
	body, err := fetch(searchURL)
	if err != nil {
		return nil
	}

	re := regexp.MustCompile(`(?is)href="([^"]+)"[^>]*class="[^"]*result__a[^"]*"`)
	matches := re.FindAllStringSubmatch(body, -1)
	var out []string
	seen := map[string]bool{}
	for _, m := range matches {
		u := html.UnescapeString(m[1])
		if strings.Contains(u, "uddg=") {
			if pu, err := url.Parse(u); err == nil {
				if t := pu.Query().Get("uddg"); t != "" {
					u = t
				}
			}
		}
		if strings.Contains(strings.ToLower(u), domain) && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func anyMatch(text string, patterns ...string) string {
	for _, p := range patterns {
		re := regexp.MustCompile(`(?i)` + p)
		if m := re.FindString(text); m != "" {
			return m
		}
	}
	return ""
}

func extractFromText(text string) (chipset, socket, header, bus, pin, module, bios string, evidence []string) {
	t := strings.ReplaceAll(text, "\u00a0", " ")
	upper := strings.ToUpper(t)

	chipsets := []string{
		`X870E`, `X870`, `B850`, `B840`, `X670E`, `X670`, `B650E`, `B650`, `A620`,
		`X570`, `B550`, `A520`, `B450`, `X470`, `B350`, `X370`,
		`Z890`, `B860`, `H810`, `Z790`, `B760`, `H770`, `H610`, `Z690`, `B660`, `H670`,
	}
	for _, c := range chipsets {
		if strings.Contains(upper, c) {
			chipset = c
			break
		}
	}
	if chipset != "" {
		evidence = append(evidence, "Chipset found on official page: "+chipset)
	}

	sockets := []string{`AM5`, `AM4`, `LGA1851`, `LGA1700`, `LGA1200`}
	for _, s := range sockets {
		if strings.Contains(upper, s) {
			socket = s
			break
		}
	}
	if socket != "" {
		evidence = append(evidence, "Socket found on official page: "+socket)
	}

	if regexp.MustCompile(`(?i)\bSPI[_ -]?TPM\b|TPM HEADER|TRUSTED PLATFORM MODULE HEADER`).MatchString(t) {
		header = "TPM header"
	}
	if regexp.MustCompile(`(?i)\bSPI[_ -]?TPM\b|\bSPI\b.{0,80}\bTPM\b|\bTPM\b.{0,80}\bSPI\b`).MatchString(t) {
		bus = "SPI"
		if header == "" {
			header = "SPI_TPM / TPM header"
		}
	}
	if regexp.MustCompile(`(?i)\bJTPM1\b|\bLPC\b.{0,80}\bTPM\b|\bTPM\b.{0,80}\bLPC\b`).MatchString(t) {
		if bus == "" {
			bus = "LPC"
		}
		if header == "" {
			header = "JTPM1 / TPM header"
		}
	}
	if header != "" {
		evidence = append(evidence, "External TPM header reference found on official page")
	}
	if bus != "" {
		evidence = append(evidence, "TPM bus reference found: "+bus)
	}

	pinPatterns := []string{
		`14\s*-\s*1\s*(?:pin|pins)?`,
		`12\s*-\s*1\s*(?:pin|pins)?`,
		`20\s*-\s*1\s*(?:pin|pins)?`,
		`14\s*pin`,
		`12\s*pin`,
		`20\s*pin`,
	}
	if m := anyMatch(t, pinPatterns...); m != "" {
		pin = strings.TrimSpace(m)
		evidence = append(evidence, "TPM pin layout found: "+pin)
	}

	modulePatterns := []string{
		`GC-TPM2\.0\s+SPI\s+V2`,
		`GC-TPM2\.0\s+SPI\s+2\.0`,
		`GC-TPM2\.0\s+SPI`,
		`MS-4136`,
		`MS-4462`,
		`TPM-M\s+R2\.0`,
		`TPM-SPI`,
	}
	if m := anyMatch(t, modulePatterns...); m != "" {
		module = strings.ToUpper(strings.TrimSpace(m))
		evidence = append(evidence, "TPM module reference found: "+module)
	}

	if regexp.MustCompile(`(?i)trusted computing|security device support|fTPM|PTT`).MatchString(t) {
		switch {
		case strings.Contains(strings.ToUpper(t), "TRUSTED COMPUTING"):
			bios = "BIOS → Security/Trusted Computing"
		case strings.Contains(strings.ToUpper(t), "FTPM"):
			bios = "BIOS → AMD fTPM / Trusted Computing"
		case strings.Contains(strings.ToUpper(t), "PTT"):
			bios = "BIOS → Intel PTT / Trusted Computing"
		}
	}
	return
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func missingFields(r *ResearchResult) {
	r.Missing = nil
	checks := []struct{ n, v string }{
		{"chipset", r.Chipset}, {"socket", r.Socket}, {"TPM header", r.Header},
		{"busz", r.Bus}, {"pin-kiosztás", r.PinLayout}, {"modul", r.Module},
	}
	for _, c := range checks {
		if strings.TrimSpace(c.v) == "" {
			r.Missing = append(r.Missing, c.n)
		}
	}
}

func researchFree(h Hardware) ResearchResult {
	if r, ok := lookupVerifiedProfile(h); ok {
		return r
	}

	maker := normMaker(h.Manufacturer)
	if maker == "" {
		maker = normMaker(h.SystemManufacturer)
	}
	model := strings.TrimSpace(h.Product)
	if model == "" {
		model = h.SystemModel
	}

	r := ResearchResult{
		Status: "pending", Manufacturer: maker, Model: model,
		Confidence: 10, Updated: time.Now().Format(time.RFC3339),
		Note: "Automatikus, költségmentes gyártói webkutatás. Emberi jóváhagyás nélkül nem válik ellenőrzött profillá.",
	}
	urls := searchOfficial(maker, model)
	for _, u := range urls {
		page, err := fetch(u)
		if err != nil {
			continue
		}
		text := stripHTML(page)
		c, s, hdr, b, p, m, bios, ev := extractFromText(text)
		if r.Chipset == "" {
			r.Chipset = c
		}
		if r.Socket == "" {
			r.Socket = s
		}
		if r.Header == "" {
			r.Header = hdr
		}
		if r.Bus == "" {
			r.Bus = b
		}
		if r.PinLayout == "" {
			r.PinLayout = p
		}
		if r.Module == "" {
			r.Module = m
		}
		if r.BIOSHint == "" {
			r.BIOSHint = bios
		}
		r.Evidence = append(r.Evidence, ev...)
		r.Sources = append(r.Sources, u)
	}
	r.Sources = dedupe(r.Sources)
	r.Evidence = dedupe(r.Evidence)
	if len(r.Sources) > 0 {
		r.Confidence += 25
	}
	fields := []string{r.Chipset, r.Socket, r.Header, r.Bus, r.PinLayout, r.Module, r.BIOSHint}
	for _, f := range fields {
		if f != "" {
			r.Confidence += 8
		}
	}
	if r.Confidence > 90 {
		r.Confidence = 90
	}
	missingFields(&r)
	return r
}

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST,GET,OPTIONS")
}

func main() {
	cfg := loadConfig()
	_ = os.MkdirAll(cfg.CacheDir, 0755)
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		db := verifiedDB()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":               true,
			"service":          "DMA Best EU Research",
			"databaseVersion":  "V32-MSI-250-EXPANSION-736-VERIFIED",
			"databaseSchema":   db.SchemaVersion,
			"verifiedProfiles": len(db.Profiles),
			"generated":        db.Generated,
			"aiRequired":       false,
		})
	})

	mux.HandleFunc("/v1/research", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != "POST" {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}

		var h Hardware
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&h); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		// Always check the embedded verified database before cache.
		// This prevents old PENDING cache entries from masking newly added VERIFIED profiles.
		if verified, ok := lookupVerifiedProfile(h); ok {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(verified)
			return
		}

		if cached, ok := loadCache(cfg, h); ok {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cached)
			return
		}

		res := researchFree(h)
		saveCache(cfg, h, res)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	})

	log.Printf("DMA Best EU FREE Research listening on %s", cfg.Listen)
	log.Printf("No OpenAI API key required")
	log.Printf("Database V32 MSI 250 EXPANSION / 736 VERIFIED active: %d profiles", len(verifiedDB().Profiles))
	log.Fatal(http.ListenAndServe(cfg.Listen, mux))
}
