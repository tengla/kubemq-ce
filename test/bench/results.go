package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Sample is one run of a scenario.
type Sample struct {
	Messages int64   `json:"messages"`
	Seconds  float64 `json:"seconds"`
	MsgsPerS float64 `json:"msgs_per_s"`
	MBPerS   float64 `json:"mb_per_s"`
	// Latencies are of one call, which can carry many messages.
	P50Micros float64 `json:"p50_us"`
	P99Micros float64 `json:"p99_us"`
	MaxMicros float64 `json:"max_us"`
	// Lost counts messages that were sent and never arrived, Failed the calls
	// that returned an error.
	Lost             int64   `json:"lost"`
	Failed           int64   `json:"failed"`
	Error            string  `json:"error,omitempty"`
	RSSKB            int64   `json:"rss_kb,omitempty"`
	ServerCPUSeconds float64 `json:"server_cpu_s,omitempty"`
}

// Result is all runs of a scenario.
type Result struct {
	Name    string   `json:"name"`
	Samples []Sample `json:"samples"`
}

type results struct {
	Label    string    `json:"label"`
	Server   string    `json:"server"`
	Started  time.Time `json:"started"`
	Repeat   int       `json:"repeat"`
	Duration string    `json:"duration"`
	Results  []Result  `json:"results"`
}

func (r *results) save(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func load(path string) (*results, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := &results{}
	return r, json.Unmarshal(data, r)
}

// latencies records one worker's call times, so workers don't share a lock.
type latencies []time.Duration

func (l *latencies) add(d time.Duration) {
	*l = append(*l, d)
}

func finish(start time.Time, messages int64, size int, recorders []latencies) Sample {
	return finishAt(time.Now(), start, messages, size, recorders)
}

func finishAt(end, start time.Time, messages int64, size int, recorders []latencies) Sample {
	seconds := end.Sub(start).Seconds()
	sample := Sample{Messages: messages, Seconds: seconds}
	if seconds > 0 {
		sample.MsgsPerS = float64(messages) / seconds
		sample.MBPerS = sample.MsgsPerS * float64(size) / 1e6
	}
	var all []time.Duration
	for _, recorder := range recorders {
		all = append(all, recorder...)
	}
	if len(all) > 0 {
		sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
		micros := func(d time.Duration) float64 { return float64(d) / float64(time.Microsecond) }
		sample.P50Micros = micros(all[len(all)/2])
		sample.P99Micros = micros(all[(len(all)-1)*99/100])
		sample.MaxMicros = micros(all[len(all)-1])
	}
	return sample
}

func median(samples []Sample, value func(Sample) float64) float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, value(sample))
	}
	sort.Float64s(values)
	if len(values) == 0 {
		return 0
	}
	return values[len(values)/2]
}

func spread(samples []Sample) (low, high float64) {
	low, high = math.Inf(1), math.Inf(-1)
	for _, sample := range samples {
		low, high = math.Min(low, sample.MsgsPerS), math.Max(high, sample.MsgsPerS)
	}
	return low, high
}

func (r Result) failed() bool {
	for _, sample := range r.Samples {
		if sample.Error != "" || sample.Failed > 0 {
			return true
		}
	}
	return false
}

// problems describes what went wrong in any run, for the end of a line.
func (r Result) problems() string {
	var lost, failed int64
	message := ""
	for _, sample := range r.Samples {
		lost += sample.Lost
		failed += sample.Failed
		if message == "" {
			message = sample.Error
		}
	}
	var parts []string
	if lost != 0 {
		parts = append(parts, fmt.Sprintf("LOST %d", lost))
	}
	if failed != 0 {
		parts = append(parts, fmt.Sprintf("FAILED %d", failed))
	}
	if message != "" {
		parts = append(parts, fmt.Sprintf("ERROR %.80s", message))
	}
	return strings.Join(parts, " ")
}

func printHeader() {
	fmt.Printf("%-42s %10s %9s %10s %10s %8s %8s\n", "scenario", "msg/s", "MB/s", "p50", "p99", "cpu s", "rss MB")
}

func micros(value float64) string {
	if value == 0 {
		return "-"
	}
	return time.Duration(value * float64(time.Microsecond)).Round(time.Microsecond).String()
}

func printResult(r Result) {
	rate := median(r.Samples, func(s Sample) float64 { return s.MsgsPerS })
	bytes := median(r.Samples, func(s Sample) float64 { return s.MBPerS })
	p50 := median(r.Samples, func(s Sample) float64 { return s.P50Micros })
	p99 := median(r.Samples, func(s Sample) float64 { return s.P99Micros })
	cpu := median(r.Samples, func(s Sample) float64 { return s.ServerCPUSeconds })
	rss := median(r.Samples, func(s Sample) float64 { return float64(s.RSSKB) / 1024 })
	fmt.Printf("%-42s %10.0f %9.1f %10s %10s %8.1f %8.0f %s\n", r.Name, rate, bytes, micros(p50), micros(p99), cpu, rss, r.problems())
}

// compare prints the change between two runs. A change marked as noise is smaller
// than the spread of the runs, so it says nothing either way.
func compare(beforePath, afterPath string) error {
	before, err := load(beforePath)
	if err != nil {
		return err
	}
	after, err := load(afterPath)
	if err != nil {
		return err
	}
	byName := make(map[string]Result)
	for _, r := range before.Results {
		byName[r.Name] = r
	}
	fmt.Printf("before: %s, server %s, %s\nafter:  %s, server %s, %s\n\n", before.Label, before.Server, before.Started.Format(time.DateTime), after.Label, after.Server, after.Started.Format(time.DateTime))
	fmt.Printf("%-42s %10s %10s %8s %6s %10s %10s\n", "scenario", "before", "after", "change", "", "p99 before", "p99 after")
	for _, a := range after.Results {
		b, ok := byName[a.Name]
		if !ok {
			continue
		}
		rate := func(s Sample) float64 { return s.MsgsPerS }
		p99 := func(s Sample) float64 { return s.P99Micros }
		beforeRate, afterRate := median(b.Samples, rate), median(a.Samples, rate)
		change := "n/a"
		if beforeRate > 0 {
			change = fmt.Sprintf("%+.0f%%", (afterRate-beforeRate)/beforeRate*100)
		}
		lowB, highB := spread(b.Samples)
		lowA, highA := spread(a.Samples)
		noise := ""
		if lowA <= highB && lowB <= highA {
			noise = "noise"
		}
		problems := strings.TrimSpace(strings.Join([]string{prefixed("before: ", b.problems()), prefixed("after: ", a.problems())}, " "))
		fmt.Printf("%-42s %10.0f %10.0f %8s %6s %10s %10s %s\n", a.Name, beforeRate, afterRate, change, noise, micros(median(b.Samples, p99)), micros(median(a.Samples, p99)), problems)
	}
	return nil
}

func prefixed(prefix, text string) string {
	if text == "" {
		return ""
	}
	return prefix + text
}

type processStats struct {
	rssKB      int64
	cpuSeconds float64
}

// readProcess asks ps, which works the same on macOS and Linux. Zero means no
// process was given or it's gone.
func readProcess(pid int) processStats {
	if pid == 0 {
		return processStats{}
	}
	output, err := exec.Command("ps", "-o", "rss=,time=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return processStats{}
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 {
		return processStats{}
	}
	rss, _ := strconv.ParseInt(fields[0], 10, 64)
	return processStats{rssKB: rss, cpuSeconds: parseCPUTime(fields[1])}
}

// parseCPUTime reads ps's time, such as 1:02.50 on macOS or 00:01:02 on Linux.
func parseCPUTime(value string) float64 {
	seconds := 0.0
	for _, part := range strings.Split(value, ":") {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0
		}
		seconds = seconds*60 + n
	}
	return seconds
}
