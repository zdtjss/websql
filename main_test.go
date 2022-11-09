package main

import (
	"fmt"
	"testing"
)

func TestFibonaci(t *testing.T) {
	var i int
	for i = 0; i < 10; i++ {
		fmt.Printf("%d\n", fibonaci(i))
	}

	fmt.Printf("%d\n", fibonaci2(500))
}

func fibonaci(i int) int {
	if i == 0 {
		return 0
	}
	if i == 1 {
		return 1
	}
	return fibonaci(i-1) + fibonaci(i-2)
}

func fibonaci2(i int) uint64 {
	var last, previous1, previous2 uint64 = 0, 1, 0
	for s := 0; s < i; s++ {
		last = previous1 + previous2
		previous2 = previous1
		previous1 = last
	}
	return last
}
