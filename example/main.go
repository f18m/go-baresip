// Package  main contains a simple app showing how to use the gobaresip module.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/f18m/go-baresip/pkg/gobaresip"
	"go.uber.org/zap"
)

// zapLogger is a concrete implementation of Logger using zap's SugaredLogger
type zapLogger struct {
	*zap.SugaredLogger
}

func main() {
	// Initialize a logger, e.g. zap
	logger, _ := zap.NewProduction()
	loggerAdapter := zapLogger{logger.Sugar()}
	loggerAdapter.Info("Golang Baresip Example starting")

	// Allocate Baresip instance with options
	gb, err := gobaresip.New(
		gobaresip.SetAudioPath("/usr/share/sounds"),
		gobaresip.SetBaresipDebug(true),
		gobaresip.SetLogger(loggerAdapter),
		gobaresip.SetLogBaresipStdoutAndStderr(true, true),
		gobaresip.SetPingInterval(6*time.Second),
	)
	if err != nil {
		loggerAdapter.Fatal(err)
	}

	// Start the Baresip instance
	// This will start the baresip process and connect to the control TCP server.
	cancelFunc, err := gb.Start()
	if err != nil {
		loggerAdapter.Errorf("failed starting baresip: %s", err)
	}

	// Process
	// - events: unsolicited messages from baresip, e.g. incoming calls, registrations, etc.
	// - responses: responses to commands sent to baresip, e.g. command results
	// reading from the 2 channels:
	eChan := gb.GetEventChan()
	rChan := gb.GetResponseChan()

	go func() {
		for {
			select {
			case e, ok := <-eChan:
				if !ok {
					continue
				}
				log.Println("EVENT: " + string(e.RawJSON))

				// your logic goes here

			case r, ok := <-rChan:
				if !ok {
					continue
				}
				log.Println("RESPONSE: " + string(r.RawJSON))

				// your logic goes here
			}
		}
	}()

	go func() {
		// Give baresip some time to init and register ua
		time.Sleep(5 * time.Second)

		if err := gb.CmdDial("012345"); err != nil {
			log.Println(err)
		}
	}()

	go func() {
		// Terminate baresip instance after 15 seconds... this is just a demo, you would normally not do this.
		time.Sleep(15 * time.Second)

		if err := gb.CmdQuit(); err != nil {
			log.Println(err)
		}
	}()

	// Show proper shutdown: we will wait for a signal (SIGINT or SIGTERM) to gracefully stop the Baresip instance.
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	go func() {
		sig := <-sigs
		log.Printf("** RECEIVED SIGNAL %v **\n", sig)
		done <- true
	}()

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	<-done
	cancelFunc()
	loggerAdapter.Info("Baresip has been stopped")

	err = gb.WaitForShutdown()
	if err != nil {
		loggerAdapter.Errorf("failed waiting baresip: %s", err)
	}

	loggerAdapter.Info("Golang Baresip Example exiting gracefully")
}
