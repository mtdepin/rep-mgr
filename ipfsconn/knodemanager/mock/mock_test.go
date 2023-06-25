package mock

import (
	"fmt"
	"sync"
	"testing"
)

func TestMockKnode(t *testing.T) {
	wg := sync.WaitGroup{}
	for i := 0; i < 500; i++ {
		wg.Add(1)
		fmt.Println("start ", i)
		go startDaemon()

	}
	wg.Wait()
}
