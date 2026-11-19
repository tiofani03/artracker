// Command hello is a tiny program with a few named functions. The symbolication
// tests cross-compile it for darwin/arm64 to get a Mach-O binary with DWARF.
package main

import "fmt"

//go:noinline
func first(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total += i * 2
	}
	return total
}

//go:noinline
func second(n int) int {
	return first(n) + 7
}

//go:noinline
func third(n int) int {
	return second(n) * 3
}

func main() {
	fmt.Println(third(10))
}
