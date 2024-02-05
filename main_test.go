package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/pprof/profile"
	"github.com/stretchr/testify/require"
)

func TestGopclntab(t *testing.T) {
	bin := mustBuild(t, "example")
	out := mustRun(t, bin)

	prof, err := profile.Parse(bytes.NewReader(out.Profile))
	require.NoError(t, err)

	bi, err := newBinInfo(bin)
	require.NoError(t, err)

	stacks, err := stackProfile(bi, prof)
	require.NoError(t, err)

	var sum int64
	for _, s := range stacks.Sample {
		sum += s.Value[1]
	}
	fmt.Printf("sum: %v\n", sum)
	fmt.Printf("out.StacksBytes: %v\n", out.StacksBytes)

	file, err := os.Create("stacks.pprof")
	require.NoError(t, err)
	defer file.Close()
	require.NoError(t, stacks.Write(file))
	// fmt.Printf("stack.String(): %v\n", stacks.String())

	// table, err := loadBinInfo(bin)
	// require.NoError(t, err)

	// md := table.moduledata()

	// for i := range table.t.Funcs {
	// 	f := &table.t.Funcs[i]
	// 	// if f.Sym.Name != "main.b" {
	// 	// 	continue
	// 	// }
	// 	fd := funcData(table.lt, uint32(i))

	// 	fi := funcInfo{
	// 		_func: uintptr(unsafe.Pointer(&fd.data[0])),
	// 		datap: &md,
	// 	}
	// 	maxSPDelta := funcMaxSPDelta(fi)
	// 	fmt.Printf("%s: %v\n", f.Sym.Name, maxSPDelta)
	// }
}

func mustBuild(t *testing.T, program string) string {
	bin := filepath.Join(t.TempDir(), program)
	cmd := exec.Command("go", "build", "-o", bin, "testdata/example.go")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Run(), out.String())
	return bin
}

func mustRun(t *testing.T, bin string) (o output) {
	cmd := exec.Command(bin)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Run(), out.String())
	require.NoError(t, json.NewDecoder(&out).Decode(&o))
	return
}

// output must match the output struct in testdata/example.go
type output struct {
	StacksBytes uint64
	Profile     []byte
}
