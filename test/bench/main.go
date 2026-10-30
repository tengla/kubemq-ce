// Command bench measures a running server over gRPC. It starts nothing itself,
// so the same tool can measure any build:
//
//	kubemq server
//	go run ./test/bench -addr localhost:50000 -label new -pid $(pgrep -x kubemq) -out new.json
//	go run ./test/bench compare old.json new.json
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	pb "github.com/kubemq-io/protobuf/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const maxCallSize = 256 << 20

// env is what scenarios need.
type env struct {
	client   pb.KubemqClient
	ctx      context.Context
	duration time.Duration
	budget   int64
	run      string
	pid      int

	before  processStats
	cleanup []func()
}

// later runs f after the scenario is measured.
func (e *env) later(f func()) {
	e.cleanup = append(e.cleanup, f)
}

// startMeasuring leaves what a scenario did so far, such as filling a channel,
// out of the server's CPU time.
func (e *env) startMeasuring() {
	e.before = readProcess(e.pid)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "compare" {
		if len(os.Args) != 4 {
			fatal("usage: bench compare <before.json> <after.json>")
		}
		if err := compare(os.Args[2], os.Args[3]); err != nil {
			fatal(err.Error())
		}
		return
	}
	addr := flag.String("addr", "localhost:50000", "gRPC address of the server")
	token := flag.String("token", "", "authentication token, if the server wants one")
	label := flag.String("label", "", "name of this run in the results")
	out := flag.String("out", "", "write the results to this JSON file")
	pid := flag.Int("pid", 0, "server process ID, to report its memory and CPU")
	filter := flag.String("run", "", "only scenarios whose name matches this regular expression")
	list := flag.Bool("list", false, "list the scenarios and exit")
	repeat := flag.Int("repeat", 3, "times to run each scenario")
	duration := flag.Duration("duration", 5*time.Second, "how long each timed scenario sends")
	budget := flag.Int64("budget", 1<<30, "bytes one scenario may store, which also limits its time")
	flag.Parse()

	var pattern *regexp.Regexp
	if *filter != "" {
		var err error
		if pattern, err = regexp.Compile(*filter); err != nil {
			fatal(err.Error())
		}
	}
	var selected []scenario
	for _, s := range scenarios() {
		if pattern == nil || pattern.MatchString(s.name) {
			selected = append(selected, s)
		}
	}
	if *list {
		for _, s := range selected {
			fmt.Println(s.name)
		}
		return
	}

	conn, err := grpc.NewClient(*addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxCallSize), grpc.MaxCallSendMsgSize(maxCallSize)))
	if err != nil {
		fatal(err.Error())
	}
	defer func() { _ = conn.Close() }()

	ctx := context.Background()
	if *token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", *token)
	}
	e := &env{client: pb.NewKubemqClient(conn), ctx: ctx, duration: *duration, budget: *budget, run: fmt.Sprintf("%x", time.Now().Unix()), pid: *pid}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	ping, err := e.client.Ping(pingCtx, &pb.Empty{})
	cancel()
	if err != nil {
		fatal(fmt.Sprintf("no server at %s: %s", *addr, err))
	}

	results := &results{Label: *label, Server: ping.Version, Started: time.Now(), Repeat: *repeat, Duration: duration.String()}
	fmt.Printf("server %s at %s, %d scenarios, %d times each\n\n", ping.Version, *addr, len(selected), *repeat)
	printHeader()
	for _, s := range selected {
		result := Result{Name: s.name}
		for i := 0; i < *repeat; i++ {
			result.Samples = append(result.Samples, runOnce(e, s))
		}
		results.Results = append(results.Results, result)
		printResult(result)
	}
	if *out != "" {
		if err := results.save(*out); err != nil {
			fatal(err.Error())
		}
		fmt.Printf("\nwrote %s\n", *out)
	}
	for _, r := range results.Results {
		if r.failed() {
			os.Exit(1)
		}
	}
}

func runOnce(e *env, s scenario) Sample {
	e.startMeasuring()
	sample, err := s.run(e)
	after := readProcess(e.pid)
	if err != nil {
		sample.Error = err.Error()
	}
	sample.RSSKB = after.rssKB
	sample.ServerCPUSeconds = after.cpuSeconds - e.before.cpuSeconds
	for _, f := range e.cleanup {
		f()
	}
	e.cleanup = nil
	return sample
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(message))
	os.Exit(1)
}
