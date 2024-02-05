# go-stack-profiler

Go stack profiler allows you to profile the size of your goroutine stacks.

## Usage

```bash
# create a stack profile from a binary and a goroutine profile
go-stack-profiler binary goroutine.pprof > stack.pprof

# list the size of all functions in the given binary in descending order
go-stack-profiler binary
```