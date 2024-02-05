package main

import (
	"bytes"
	"debug/elf"
	"debug/gosym"
	"debug/macho"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"
	"unsafe"

	"github.com/google/pprof/profile"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Usage = func() {
		fmt.Println("Usage: go-stack-profile <binary> [<profile>]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		return fmt.Errorf("expected at least 1 argument, got %d", flag.NArg())
	}

	bi, err := newBinInfo(flag.Arg(0))
	if err != nil {
		return fmt.Errorf("failed to parse binary: %v", err)
	}

	if flag.NArg() < 2 {
		for _, name := range bi.funcNames() {
			msp := bi.maxSPDelta(name)
			fmt.Printf("%s: %d\n", name, msp)
		}
		return nil
	}

	data, err := os.ReadFile(flag.Arg(1))
	if err != nil {
		return err
	}
	goroutines, err := profile.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to parse profile: %v", err)
	}

	stacks, err := stackProfile(bi, goroutines)
	if err != nil {
		return err
	}
	return stacks.Write(os.Stdout)
}

func stackProfile(bi *binInfo, goroutines *profile.Profile) (*profile.Profile, error) {
	stacks := goroutines.Copy()
	stacks.Sample = nil
	stacks.SampleType = append(stacks.SampleType, &profile.ValueType{Type: "goroutine_space", Unit: "bytes"})

	maxSPDeltaMap := map[string]int{}
	maxSPDelta := func(fn string) int {
		if msp, ok := maxSPDeltaMap[fn]; ok {
			return msp
		}
		msp := bi.maxSPDelta(fn)
		maxSPDeltaMap[fn] = msp
		return msp
	}

	freeFunc := &profile.Function{ID: uint64(len(stacks.Function) + 1), Name: "<free stack space>"}
	stacks.Function = append(stacks.Function, freeFunc)
	freeLoc := &profile.Location{ID: uint64(len(stacks.Location) + 1), Line: []profile.Line{{Function: freeFunc}}}
	stacks.Location = append(stacks.Location, freeLoc)

	for _, s := range goroutines.Sample {
		for i := range s.Location {
			newS := sampleWith(s, nil, []int64{s.Value[0], 0})
			var stackUsed int64
			for j := range s.Location[i:] {
				l := s.Location[i+j]
				fn := l.Line[len(l.Line)-1].Function.Name
				msp := uint64(maxSPDelta(fn))
				frameSize := int64(msp) * s.Value[0]
				stackUsed += int64(msp)
				newS.Location = append(newS.Location, l)
				if j == 0 {
					newS.Value[1] = frameSize
				}
			}
			if i == 0 {
				stackSize := max(roundUpToNextPow2(stackUsed), 2048)
				locs := make([]*profile.Location, len(s.Location)+1)
				copy(locs[1:], s.Location)
				locs[0] = freeLoc
				freeS := sampleWith(s, locs, []int64{s.Value[0], s.Value[0] * (stackSize - stackUsed)})
				stacks.Sample = append(stacks.Sample, freeS)
			}
			stacks.Sample = append(stacks.Sample, newS)
		}
	}
	return stacks, nil
}

func sampleWith(s *profile.Sample, loc []*profile.Location, val []int64) *profile.Sample {
	newS := *s
	newS.Location = loc
	newS.Value = val
	return &newS
}

// roundUpToNextPow2 rounds up a given number to the next power of 2
func roundUpToNextPow2(num int64) int64 {
	power := int64(1)
	for power < num {
		power <<= 1
	}
	return int64(power)
}

func newBinInfo(bin string) (*binInfo, error) {
	textAddr, gopclntab, err := loadElf(bin)
	if err != nil {
		textAddr, gopclntab, err = loadMacho(bin)
	}
	if err != nil {
		return nil, err
	}
	lt := gosym.NewLineTable(gopclntab, textAddr)
	table, err := gosym.NewTable(nil, lt)
	if err != nil {
		return nil, err
	}

	funcs := make(map[string]int)
	for i, f := range table.Funcs {
		funcs[f.Name] = i
	}

	r := reflect.ValueOf(lt).Elem()
	f := r.FieldByName("pctab")
	f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
	md := moduledata{pctab: f.Bytes()}

	return &binInfo{t: table, lt: lt, md: md, funcs: funcs}, nil
}

func loadMacho(bin string) (textAddr uint64, gopclntab []byte, err error) {
	var file *macho.File
	file, err = macho.Open(bin)
	if err != nil {
		return
	}
	for _, seg := range file.Sections {
		if strings.Contains(seg.Name, "text") {
			// can there be more than one text section?
			textAddr = seg.Addr
		}
	}
	if textAddr == 0 {
		err = errors.New("could not find text addr")
		return
	}
	for _, seg := range file.Sections {
		if strings.Contains(seg.Name, "gopclntab") {
			gopclntab, err = seg.Data()
			return
		}
	}
	err = errors.New("could not find gopclntab")
	return
}

func loadElf(bin string) (textAddr uint64, gopclntab []byte, err error) {
	var file *elf.File
	file, err = elf.Open(bin)
	if err != nil {
		return
	}
	for _, seg := range file.Sections {
		if strings.Contains(seg.Name, "text") {
			// can there be more than one text section?
			textAddr = seg.Addr
		}
	}
	if textAddr == 0 {
		err = errors.New("could not find text addr")
		return
	}
	for _, seg := range file.Sections {
		if strings.Contains(seg.Name, "gopclntab") {
			gopclntab, err = seg.Data()
			return
		}
	}
	err = errors.New("could not find gopclntab")
	return
}

// binInfo holds information about a Go binary.
type binInfo struct {
	lt    *gosym.LineTable
	t     *gosym.Table
	md    moduledata
	funcs map[string]int
}

func (b *binInfo) funcNames() []string {
	names := make([]string, 0, len(b.t.Funcs))
	for _, f := range b.t.Funcs {
		names = append(names, f.Name)
	}
	return names
}

func (b *binInfo) maxSPDelta(fn string) int {
	i, ok := b.funcs[fn]
	if !ok {
		return 0
	}
	fd := funcData(b.lt, uint32(i))

	fi := funcInfo{
		_func: uintptr(unsafe.Pointer(&fd.data[0])),
		datap: &b.md,
	}
	return int(funcMaxSPDelta(fi))
}

//go:linkname funcMaxSPDelta runtime.funcMaxSPDelta
func funcMaxSPDelta(f funcInfo) int32

//go:linkname funcData debug/gosym.(*LineTable).funcData
func funcData(*gosym.LineTable, uint32) lineTableFuncData

// funcInfo must match the layout of runtime.funcInfo.
type funcInfo struct {
	_func uintptr
	datap *moduledata
}

// lineTableFuncData must match the layout of debug/gosym.funcData.
type lineTableFuncData struct {
	t    uintptr // *LineTable
	data []byte  //
}

// moduledata must match the layout of runtime.moduledata.
type moduledata struct {
	// not used, just needed to get the offset of pcdata right
	pcHeader    uintptr
	funcnametab []byte
	cutab       []uint32
	filetab     []byte

	// the field we want
	pctab []byte
}
