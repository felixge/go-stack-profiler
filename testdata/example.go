package main

import (
	"bytes"
	"encoding/json"
	"os"
	"runtime/metrics"
	"runtime/pprof"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(2)
		go oneHundredBytes(&wg)
		go twoHundredBytes(&wg)
	}
	wg.Wait()

	time.Sleep(10 * time.Millisecond)

	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/stacks:bytes"},
		{Name: "/gc/stack/starting-size:bytes"},
	}
	metrics.Read(samples)

	var profile bytes.Buffer
	pprof.Lookup("goroutine").WriteTo(&profile, 0)
	json.NewEncoder(os.Stdout).Encode(output{
		Profile:     profile.Bytes(),
		StacksBytes: samples[0].Value.Uint64(),
	})
}

type output struct {
	StacksBytes uint64
	Profile     []byte
}

//go:noinline
func oneHundredBytes(wg *sync.WaitGroup) [100]byte {
	var a [100]byte
	wg.Done()
	blockForever()
	return a
}

//go:noinline
func twoHundredBytes(wg *sync.WaitGroup) [200]byte {
	var b [200]byte
	wg.Done()
	blockForever()
	return b
}

func blockForever() {
	<-make(chan struct{})
}
