package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultServer = "https://dmabest-research-server.onrender.com"

type Hardware struct {
	Manufacturer string `json:"manufacturer"`
	Product      string `json:"product"`
	Version      string `json:"version"`
	CPU          string `json:"cpu,omitempty"`
	BIOS         string `json:"bios,omitempty"`
}

type Result struct {
	Status       string   `json:"status"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	Chipset      string   `json:"chipset"`
	Socket       string   `json:"socket"`
	Header       string   `json:"header"`
	Bus          string   `json:"bus"`
	PinLayout    string   `json:"pinLayout"`
	Module       string   `json:"module"`
	Confidence   int      `json:"confidence"`
	Sources      []string `json:"sources"`
	Evidence     []string `json:"evidence"`
	Missing      []string `json:"missing"`
	Note         string   `json:"note"`
}

type Expected struct {
	Status         string   `json:"status"`
	Chipset        string   `json:"chipset"`
	Socket         string   `json:"socket"`
	Header         string   `json:"header"`
	Bus            string   `json:"bus"`
	ModuleContains []string `json:"moduleContains"`
	ConfidenceMin  int      `json:"confidenceMin,omitempty"`
}

type TestCase struct {
	Name     string   `json:"name"`
	Hardware Hardware `json:"hardware"`
	Expected Expected `json:"expected"`
}

type TestOutcome struct {
	Index    int      `json:"index"`
	Name     string   `json:"name"`
	Pass     bool     `json:"pass"`
	Errors   []string `json:"errors,omitempty"`
	Duration string   `json:"duration"`
	Result   Result   `json:"result"`
}

type Summary struct {
	Server      string        `json:"server"`
	StartedAt   string        `json:"startedAt"`
	FinishedAt  string        `json:"finishedAt"`
	Total       int           `json:"total"`
	Passed      int           `json:"passed"`
	Failed      int           `json:"failed"`
	Verified    int           `json:"verified"`
	Pending     int           `json:"pending"`
	OtherStatus int           `json:"otherStatus"`
	Outcomes    []TestOutcome `json:"outcomes"`
}

func fallbackTests() []TestCase {
	return []TestCase{
		{
			Name:     "GIGABYTE X870E AORUS PRO ICE Rev 1.0",
			Hardware: Hardware{Manufacturer: "Gigabyte Technology Co., Ltd.", Product: "X870E AORUS PRO ICE", Version: "1.0"},
			Expected: Expected{Status: "verified", Chipset: "AMD X870E", Socket: "AM5", Header: "SPI_TPM", Bus: "SPI", ModuleContains: []string{"GC-TPM2.0 SPI", "GC-TPM2.0 SPI 2.0", "GC-TPM2.0 SPI V2"}, ConfidenceMin: 100},
		},
		{
			Name:     "GIGABYTE X870E AORUS PRO ICE Rev 1.1",
			Hardware: Hardware{Manufacturer: "Gigabyte Technology Co., Ltd.", Product: "X870E AORUS PRO ICE", Version: "1.1"},
			Expected: Expected{Status: "verified", Chipset: "AMD X870E", Socket: "AM5", Header: "SPI_TPM", Bus: "SPI", ModuleContains: []string{"GC-TPM2.0 SPI V2"}, ConfidenceMin: 100},
		},
		{
			Name:     "GIGABYTE X870E AORUS PRO ICE unknown revision",
			Hardware: Hardware{Manufacturer: "Gigabyte Technology Co., Ltd.", Product: "X870E AORUS PRO ICE", Version: "x.x"},
			Expected: Expected{Status: "pending", Chipset: "AMD X870E", Socket: "AM5", Header: "SPI_TPM", Bus: "SPI", ModuleContains: []string{"Revision required"}},
		},
	}
}

func loadTests() ([]TestCase, string) {
	for _, fn := range []string{"testcases.json", "motherboard-testcases.json"} {
		if b, err := os.ReadFile(fn); err == nil {
			var t []TestCase
			if json.Unmarshal(b, &t) == nil && len(t) > 0 {
				return t, fn
			}
		}
	}
	return fallbackTests(), "built-in fallback"
}

func post(server string, h Hardware) (Result, error) {
	b, _ := json.Marshal(h)
	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequest("POST", strings.TrimRight(server, "/")+"/v1/research", bytes.NewReader(b))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out Result
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, fmt.Errorf("JSON parse: %v | %s", err, string(body))
	}
	return out, nil
}

func eq(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func validate(tc TestCase, r Result) []string {
	var e []string
	if tc.Expected.Status != "" && !eq(r.Status, tc.Expected.Status) {
		e = append(e, fmt.Sprintf("status: got %q want %q", r.Status, tc.Expected.Status))
	}
	if tc.Expected.Chipset != "" && !eq(r.Chipset, tc.Expected.Chipset) {
		e = append(e, fmt.Sprintf("chipset: got %q want %q", r.Chipset, tc.Expected.Chipset))
	}
	if tc.Expected.Socket != "" && !eq(r.Socket, tc.Expected.Socket) {
		e = append(e, fmt.Sprintf("socket: got %q want %q", r.Socket, tc.Expected.Socket))
	}
	if tc.Expected.Header != "" && !eq(r.Header, tc.Expected.Header) {
		e = append(e, fmt.Sprintf("header: got %q want %q", r.Header, tc.Expected.Header))
	}
	if tc.Expected.Bus != "" && !eq(r.Bus, tc.Expected.Bus) {
		e = append(e, fmt.Sprintf("bus: got %q want %q", r.Bus, tc.Expected.Bus))
	}
	if tc.Expected.ConfidenceMin > 0 && r.Confidence < tc.Expected.ConfidenceMin {
		e = append(e, fmt.Sprintf("confidence: got %d want >= %d", r.Confidence, tc.Expected.ConfidenceMin))
	}
	for _, s := range tc.Expected.ModuleContains {
		if !strings.Contains(strings.ToLower(r.Module), strings.ToLower(s)) {
			e = append(e, fmt.Sprintf("module missing %q (got %q)", s, r.Module))
		}
	}
	return e
}

func saveReports(sum Summary) error {
	b, _ := json.MarshalIndent(sum, "", "  ")
	if err := os.WriteFile("DMABest-Test-Report.json", b, 0644); err != nil {
		return err
	}

	f, err := os.Create("DMABest-Test-Report.csv")
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	_ = w.Write([]string{
		"index", "pass", "name", "status", "confidence", "chipset", "socket",
		"header", "bus", "module", "duration", "errors",
	})
	for _, o := range sum.Outcomes {
		_ = w.Write([]string{
			strconv.Itoa(o.Index + 1),
			strconv.FormatBool(o.Pass),
			o.Name,
			o.Result.Status,
			strconv.Itoa(o.Result.Confidence),
			o.Result.Chipset,
			o.Result.Socket,
			o.Result.Header,
			o.Result.Bus,
			o.Result.Module,
			o.Duration,
			strings.Join(o.Errors, " | "),
		})
	}
	return w.Error()
}

func main() {
	server := defaultServer
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "http") {
		server = os.Args[1]
	}

	tests, source := loadTests()
	started := time.Now()

	fmt.Println("DMA BEST EU - MASS DATABASE/API TESTER V2")
	fmt.Println("Server:", server)
	fmt.Println("Test source:", source)
	fmt.Printf("Profiles/tests: %d\n", len(tests))
	fmt.Println("Parallel workers: 3")
	fmt.Println("------------------------------------------------------------")

	outcomes := make([]TestOutcome, len(tests))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var printMu sync.Mutex

	worker := func() {
		defer wg.Done()
		for idx := range jobs {
			tc := tests[idx]
			t0 := time.Now()
			r, err := post(server, tc.Hardware)
			out := TestOutcome{Index: idx, Name: tc.Name, Result: r}
			if err != nil {
				out.Pass = false
				out.Errors = []string{err.Error()}
			} else {
				out.Errors = validate(tc, r)
				out.Pass = len(out.Errors) == 0
			}
			out.Duration = time.Since(t0).Round(time.Millisecond).String()
			outcomes[idx] = out

			printMu.Lock()
			if out.Pass {
				fmt.Printf("[%d/%d] PASS  %s | %s | %s\n", idx+1, len(tests), tc.Name, r.Status, r.Module)
			} else {
				fmt.Printf("[%d/%d] FAIL  %s\n", idx+1, len(tests), tc.Name)
				for _, e := range out.Errors {
					fmt.Println("          -", e)
				}
			}
			printMu.Unlock()
		}
	}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go worker()
	}
	for i := range tests {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	sum := Summary{
		Server:     server,
		StartedAt:  started.Format(time.RFC3339),
		FinishedAt: time.Now().Format(time.RFC3339),
		Total:      len(tests),
		Outcomes:   outcomes,
	}
	for _, o := range outcomes {
		if o.Pass {
			sum.Passed++
		} else {
			sum.Failed++
		}
		switch strings.ToLower(strings.TrimSpace(o.Result.Status)) {
		case "verified":
			sum.Verified++
		case "pending":
			sum.Pending++
		case "":
		default:
			sum.OtherStatus++
		}
	}

	_ = saveReports(sum)

	fmt.Println("------------------------------------------------------------")
	fmt.Printf("RESULT: %d/%d PASSED\n", sum.Passed, sum.Total)
	fmt.Printf("FAILED: %d\n", sum.Failed)
	fmt.Printf("VERIFIED RESPONSES: %d\n", sum.Verified)
	fmt.Printf("PENDING RESPONSES: %d\n", sum.Pending)
	fmt.Println("Reports saved:")
	fmt.Println(" - DMABest-Test-Report.csv")
	fmt.Println(" - DMABest-Test-Report.json")
	if sum.Failed == 0 {
		fmt.Println("\nALL TESTS PASSED")
	}
	fmt.Println("\nPress ENTER to close...")
	fmt.Scanln()
}
