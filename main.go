package main

import (
	_ "fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/judwhite/go-svc/svc"
)

// var (
// 	gApp = NewApplication()
// )

// program implements svc.Service
type program struct {
	LogFile *os.File
	svr     *server
}

func (p *program) Init(env svc.Environment) error {

	log.Printf("is win service? %v\n", env.IsWindowsService())

	// write to "example.log" when running as a Windows Service

	if env.IsWindowsService() {

		dir, err := filepath.Abs(filepath.Dir(os.Args[0]))

		if err != nil {

			return err

		}

		logPath := filepath.Join(dir, "example.log")

		f, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)

		if err != nil {

			return err

		}

		p.LogFile = f

		log.SetOutput(f)

	}

	return nil

}

func (p *program) Start() error {

	log.Printf("Starting...\n")

	go p.svr.start()

	return nil

}

func (p *program) Stop() error {

	log.Printf("Stopping...\n")

	if err := p.svr.stop(); err != nil {

		return err

	}

	log.Printf("Stopped.\n")

	return nil

}

func main() {

	prg := program{
		svr: &server{},
	}

	defer func() {

		if prg.LogFile != nil {

			if closeErr := prg.LogFile.Close(); closeErr != nil {
				log.Printf("error closing '%s': %v\n", prg.LogFile.Name(), closeErr)
			}
		}
	}()

	// call svc.Run to start your program/service
	// svc.Run will call Init, Start, and Stop
	if err := svc.Run(&prg); err != nil {
		log.Fatal(err)
	}

	// Incubate this program, including the command-line options, the OS signals
	//incubator.Incubate(gApp)
	//gApp.Run()
}
