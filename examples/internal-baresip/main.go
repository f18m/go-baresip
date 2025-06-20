// Package  main contains a simple app showing how to use the gobaresip module.
package main

import (
	"context"
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

// BaresipStartOptions holds the options for starting the internal baresip instance.
type BaresipStartOptions struct {
	// Name of the SIP user agent
	UserAgent string
	// Path to the baresip configuration directory. It defaults to the $HOME directory.
	ConfigPath string
	// Path to the audio files directory. It defaults to the current directory.
	AudioPath string
	// Debug mode. If true, it enables debug logging by baresip.
	Debug bool
}

func main() {
	// Initialize a logger, e.g. zap
	logger, _ := zap.NewProduction()
	loggerAdapter := zapLogger{logger.Sugar()}
	loggerAdapter.Info("Golang Baresip Example starting")

	// Allocate Baresip instance with options
	gb, err := gobaresip.New(
		gobaresip.SetInternalBaresipStartupOptions(
			gobaresip.BaresipStartOptions{
				AudioPath: "/usr/share/sounds",
				UserAgent: "gobaresip-example",
				Debug:     true,
			},
		),
		gobaresip.SetLogger(loggerAdapter),
		gobaresip.CaptureInternalBaresipStdoutStderr(true, true),
		gobaresip.SetPingInterval(6*time.Second),
	)
	if err != nil {
		loggerAdapter.Fatal(err)
	}

	// Serve the Baresip instance
	// This will start the baresip process and connect to the control TCP server.
	// The Baresip instance can be terminated at any time using the baresipCancel() function.
	// Communication happens using the event/response channels... keep reading
	baresipCtx, baresipCancel := context.WithCancel(context.Background())
	go func() {
		err := gb.Serve(baresipCtx)
		if err != nil {
			loggerAdapter.Errorf("baresip exit error: %s", err)
		}
	}()

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
		// Give baresip some time to init and register SIP User Agent
		time.Sleep(5 * time.Second)

		// Dial a dummy phone number
		if err := gb.CmdDial("012345"); err != nil {
			log.Println(err)
		}
	}()

	go func() {
		time.Sleep(15 * time.Second)

		// Terminate baresip instance after 15 seconds... this is just a demo, you would normally not do this.
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

	loggerAdapter.Info("Golang Baresip Example: waiting for CTRL+C or SIGTERM to exit...")
	<-done
	baresipCancel()
	loggerAdapter.Info("Golang Baresip Example exiting gracefully")
}
