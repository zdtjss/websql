package main

import (
	"fmt"
	"testing"
	"time"
)

func TestFibonaci(t *testing.T) {
	var i int
	for i = 0; i < 10; i++ {
		fmt.Printf("%d\n", fibonaci(i))
	}

	fmt.Printf("%d\n", fibonaci2(500))
}

func TestRecover(t *testing.T) {

	go func() {
		defer func() {
			if err := recover(); err != nil {
				t.Errorf("%s", err)
			}
		}()
		raisePanic()
	}()
	go func() {
		defer func() {
			if err := recover(); err != nil {
				t.Errorf("%s", err)
			}
		}()
		raisePanic()
	}()
	time.Sleep(time.Duration(1000))
	t.Logf("%s", "还是可以执行的，应该能说明panic是goroutine层的活动")
	// var ch chan int = make(chan int, 10)
	// close(ch)
	// ch <- 1
}

func raisePanic() {
	var i, j uint8 = 0, 0
	c := i / j
	fmt.Print(c)
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
