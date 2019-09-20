package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/zoidbergwill/hdrhistogram"
)

type bmStat struct {
	errs          map[string]int
	reqs          int64
	badStatusCode int64
	latency       *hdrhistogram.Histogram
}

func newBmStat() *bmStat {
	return &bmStat{
		latency: hdrhistogram.New(0, int64(config.bmDuration), 5),
	}
}

func (bs *bmStat) AddErr(err error) {
	if bs.errs == nil {
		bs.errs = map[string]int{}
	}

	desc := err.Error()
	if count, found := bs.errs[desc]; found {
		bs.errs[desc] = count + 1
	} else {
		bs.errs[desc] = 1
	}
}

func (bs *bmStat) Merge(other *bmStat) {
	bs.reqs += other.reqs
	bs.badStatusCode += other.badStatusCode
	bs.latency.Merge(other.latency)

	if other.errs == nil {
		return
	}

	if bs.errs == nil {
		bs.errs = map[string]int{}
	}

	for desc, a := range other.errs {
		if b, found := bs.errs[desc]; found {
			bs.errs[desc] = a + b
		} else {
			bs.errs[desc] = a
		}
	}
}

func (bs *bmStat) PrintErr(out io.Writer) {
	if bs.errs == nil {
		return
	}
	fmt.Fprintf(out, "  Errors:\n")
	for n, c := range bs.errs {
		fmt.Fprintf(out, "\t%s\t%d\n", n, c)
	}
}

func formatLatencyDuration(v float64) string {
	return time.Duration(v).Round(10 * time.Microsecond).String()
}

func (bs *bmStat) PrintLatency(out io.Writer) {
	lat := bs.latency
	avg := lat.Mean()
	stdev := lat.StdDev()
	max := lat.Max()
	up := int64(avg + stdev)
	low := int64(avg - stdev)

	count := int64(0)
	// use the leftmost value(From) to represent the range
	for _, bar := range lat.Distribution() {
		if bar.From >= up {
			break
		}
		if bar.From >= low {
			count += bar.Count
		}
	}
	inStdevRate := float64(count*10000/bs.reqs) / 100

	table := [][]string{
		{"Item", "Avg", "Stdev", "Max", "+/-Stdev"},
		{"Latency",
			formatLatencyDuration(avg),
			formatLatencyDuration(stdev),
			formatLatencyDuration(float64(max)),
			fmt.Sprintf("%0.2f%%", inStdevRate),
		},
		// Is Req/s distribution important enough?
	}
	for _, row := range table {
		fmt.Fprintf(out, "  %10s %8s %10s %9s %10s\n",
			row[0], row[1], row[2], row[3], row[4])
	}
	fmt.Fprintln(out, "  Latency Distribution")
	percents := []float64{50, 75, 90, 95, 99, 99.5, 99.9}
	for _, p := range percents {
		fmt.Fprintf(out, "    %0.1f%%\t%s\n", p,
			formatLatencyDuration(float64(lat.ValueAtQuantile(p))))
	}
}

func (bs *bmStat) PrintBadStatusCode(out io.Writer) {
	if bs.badStatusCode == 0 {
		return
	}
	fmt.Fprintf(out, "  Non-2xx or 3xx responses: %d\n", bs.badStatusCode)
}

func (bs *bmStat) IncrReq() {
	bs.reqs++
}

func (bs *bmStat) IncrBadStatusCode() {
	bs.badStatusCode++
}

func printStats(timeUsed time.Duration, stats []*bmStat, out io.Writer) {
	total := stats[0]
	for i := 1; i < len(stats); i++ {
		total.Merge(stats[i])
	}
	fmt.Fprintf(out, "  %d requests in %v\n", total.reqs, timeUsed)
	total.PrintLatency(out)
	total.PrintBadStatusCode(out)
	total.PrintErr(out)
	fmt.Fprintf(out, "Requests/sec:    %f\n", float64(total.reqs)/timeUsed.Seconds())
}

type reqResult struct {
	err        error
	statusCode int
	time       time.Duration
}

type reqCtx struct {
	res    *reqResult
	cancel context.CancelFunc
}

func aggregateStatFromReqCtx(stat *bmStat, ctx *reqCtx) {
	f := ctx.cancel
	if f != nil {
		f()
	}
	res := ctx.res
	stat.IncrReq()
	if res.err != nil {
		stat.AddErr(res.err)
	} else if res.statusCode < 200 || res.statusCode >= 400 {
		stat.IncrBadStatusCode()
	}
	// count latency even when the request failed (connect/read timeout, etc.)
	err := stat.latency.RecordValue(int64(res.time))
	if err != nil {
		warn(err.Error())
	}
}

func runReqsInParallel(hclient *http.Client, pStat **bmStat, wg *sync.WaitGroup,
	cancelled <-chan struct{}) {

	defer wg.Done()
	stat := newBmStat()
	*pStat = stat
	latch := make(chan struct{}, config.bmReqPerConn)
	reqCtxCh := make(chan *reqCtx, config.bmReqPerConn*2)
	timer := time.NewTimer(config.bmDuration)

	var reqWg sync.WaitGroup
	for {
		select {
		case latch <- struct{}{}:
			reqWg.Add(1)
			go func() {
				reqRes := reqResult{}
				req, cancel, err := createReq()
				if err != nil {
					// failed to prepare the request body? stop the benchmark immediately
					fatal(err.Error())
				}

				reqStart := time.Now()
				resp, err := hclient.Do(req)
				if err != nil {
					goto failed
				}

				err = readResp(req, resp, ioutil.Discard)
				if err != nil {
					goto failed
				}

				reqRes.statusCode = resp.StatusCode
				goto finished
			failed:
				reqRes.err = err
			finished:
				reqRes.time = time.Since(reqStart)
				<-latch
				reqWg.Done()
				reqCtxCh <- &reqCtx{&reqRes, cancel}
			}()

		case ctx := <-reqCtxCh:
			aggregateStatFromReqCtx(stat, ctx)

		case <-timer.C:
			// also count requests which are started but not finished
			reqWg.Wait()
			for {
				select {
				case ctx := <-reqCtxCh:
					aggregateStatFromReqCtx(stat, ctx)
				default:
					goto endloop
				}
			}

		case <-cancelled:
			// don't wait started requests if cancelled
			goto endloop
		}
	}
endloop:
}
