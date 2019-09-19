package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sync"
	"time"
)

type bmStat struct {
	errs map[string]int
	reqs int64
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

func (bs *bmStat) MergeErr(other *bmStat) {
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

func (bs *bmStat) IncrReq() {
	bs.reqs++
}

func printStats(timeUsed time.Duration, stats []bmStat, out io.Writer) {
	total := &bmStat{}
	for _, stat := range stats {
		total.reqs += stat.reqs
		total.MergeErr(&stat)
	}
	fmt.Fprintf(out, "  %d requests in %v\n", total.reqs, timeUsed)
	total.PrintErr(out)
	fmt.Fprintf(out, "Requests/sec:    %f\n", float64(total.reqs)/timeUsed.Seconds())
}

type reqResult struct {
	err error
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
	}
}

func runReqsInParallel(hclient *http.Client, stat *bmStat, wg *sync.WaitGroup,
	cancelled <-chan struct{}) {

	defer wg.Done()
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

				resp, err := hclient.Do(req)
				if err != nil {
					goto failed
				}

				err = readResp(req, resp, ioutil.Discard)
				if err != nil {
					goto failed
				}

				goto finished
			failed:
				reqRes.err = err
			finished:
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
