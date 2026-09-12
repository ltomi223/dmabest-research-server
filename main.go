package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

type Config struct {
	OpenAIAPIKey string
	Model        string
	Listen       string
	CacheDir     string
}

var cacheMu sync.Mutex

func loadConfig() Config {
	c := Config{
		Model:    "gpt-6-astra",
		Listen:   "0.0.0.0:8787",
		CacheDir: "data/cache",
	}
	c.OpenAIAPIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if v := strings.TrimSpace(os.Getenv("DMABEST_MODEL")); v != "" {
		c.Model = v
	}
	if v := strings.TrimSpace(os.Getenv("DMABEST_CACHE_DIR")); v != "" {
		c.CacheDir = v
	}
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		c.Listen = "0.0.0.0:" + p
	} else if v := strings.TrimSpace(os.Getenv("DMABEST_LISTEN")); v != "" {
		c.Listen = v
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
	case strings.Contains(u, "HP") || strings.Contains(u, "HEWLETT"):
		return "HP"
	case strings.Contains(u, "CLEVO"):
		return "CLEVO"
	case strings.Contains(u, "XMG"):
		return "XMG"
	}
	return strings.TrimSpace(s)
}

func officialDomains(maker string) string {
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
		return "support.hp.com, hp.com"
	case "CLEVO":
		return "clevo.com.tw"
	case "XMG":
		return "xmg.gg"
	default:
		return "the official manufacturer website only"
	}
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

func parseOutputText(root map[string]any) string {
	if v, ok := root["output_text"].(string); ok && v != "" {
		return v
	}
	out, _ := root["output"].([]any)
	var parts []string
	for _, item := range out {
		m, _ := item.(map[string]any)
		content, _ := m["content"].([]any)
		for _, ci := range content {
			cm, _ := ci.(map[string]any)
			if t, ok := cm["text"].(string); ok && t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	a := strings.Index(s, "{")
	b := strings.LastIndex(s, "}")
	if a >= 0 && b > a {
		return s[a : b+1]
	}
	return s
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
	// Research cache is kept for 30 days.
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

func researchWithOpenAI(cfg Config, h Hardware) (ResearchResult, error) {
	maker := normMaker(h.Manufacturer)
	if maker == "" {
		maker = normMaker(h.SystemManufacturer)
	}
	model := strings.TrimSpace(h.Product)
	if model == "" {
		model = h.SystemModel
	}

	prompt := fmt.Sprintf(`You are the DMA Best EU motherboard compatibility research service.

Research this EXACT detected system:
Manufacturer: %s
Baseboard model: %s
Baseboard version/revision: %s
System manufacturer: %s
System model: %s
CPU: %s
BIOS: %s

Use web search. Use ONLY official manufacturer sources, especially %s.
Never use Reddit, forums, stores, eBay, Amazon, blogs, reseller pages or AI-generated summaries as evidence.

Rules:
- Distinguish desktop motherboards from OEM/laptop systems.
- For laptops/OEM systems, do NOT assume an external TPM header exists.
- Never invent pin count, bus type, TPM module, chipset, socket, or BIOS path.
- If an exact fact is not supported by an official source, return an empty string for that field.
- A result is ALWAYS pending until a human approves it.
- Prefer exact model + exact revision evidence.
- Direct official manual/specification URLs should be included in sources.

Return ONLY one JSON object with EXACTLY these keys:
status, manufacturer, model, chipset, socket, header, bus, pinLayout, module, biosHint, confidence, sources, evidence, missing, note, updated

status must be "pending".
confidence is 0-95.
sources is an array of direct official URLs used.
evidence is an array of short paraphrased facts.
note must state that human approval is required before the result becomes verified.`,
		maker, model, h.Version, h.SystemManufacturer, h.SystemModel, h.CPU, h.BIOS, officialDomains(maker))

	payload := map[string]any{
		"model": cfg.Model,
		"tools": []map[string]any{{"type": "web_search"}},
		"include": []string{"web_search_call.action.sources"},
		"input": prompt,
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/responses", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAIAPIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ResearchResult{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode >= 300 {
		return ResearchResult{}, fmt.Errorf("OpenAI API HTTP %d: %s", resp.StatusCode, string(body))
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return ResearchResult{}, err
	}
	txt := extractJSONObject(parseOutputText(root))
	var r ResearchResult
	if err := json.Unmarshal([]byte(txt), &r); err != nil {
		return ResearchResult{}, fmt.Errorf("invalid research JSON: %v; text=%s", err, txt)
	}

	r.Status = "pending"
	if r.Manufacturer == "" {
		r.Manufacturer = maker
	}
	if r.Model == "" {
		r.Model = model
	}
	if r.Updated == "" {
		r.Updated = time.Now().Format(time.RFC3339)
	}
	if r.Confidence > 95 {
		r.Confidence = 95
	}
	if r.Confidence < 0 {
		r.Confidence = 0
	}
	missingFields(&r)
	if r.Note == "" {
		r.Note = "Automatikusan előkészített profil. Emberi jóváhagyás nélkül nem válik ellenőrzött profillá."
	}
	return r, nil
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"model": cfg.Model,
			"aiConfigured": cfg.OpenAIAPIKey != "",
			"service": "DMA Best EU Central Research",
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

		if cached, ok := loadCache(cfg, h); ok {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cached)
			return
		}

		if cfg.OpenAIAPIKey == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "central research service is not configured",
			})
			return
		}

		rr, err := researchWithOpenAI(cfg, h)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		saveCache(cfg, h, rr)
		_ = json.NewEncoder(w).Encode(rr)
	})

	log.Printf("DMA Best EU Central Research listening on %s", cfg.Listen)
	log.Printf("AI configured: %v; model: %s", cfg.OpenAIAPIKey != "", cfg.Model)
	log.Fatal(http.ListenAndServe(cfg.Listen, mux))
}
