package main

import (
	"sync"

	"github.com/firnsan/incubator"
)

type server struct {
	data chan int
	exit chan struct{}
	wg   sync.WaitGroup
}

var (
	gApp = NewApplication()
)

func (s *server) start() {

	s.data = make(chan int)

	s.exit = make(chan struct{})

	s.wg.Add(2)

	incubator.Incubate(gApp)
	gApp.Run()
}

func (s *server) stop() error {

	close(s.exit)

	s.wg.Wait()

	return nil
}
