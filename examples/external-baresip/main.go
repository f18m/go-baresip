// Package main contains a simple app showing how to use the gobaresip module
// assuming you have a "baresip" process running externally with the ctrl_tcp module enabled.
package main

import (
	"context"
	"errors"
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
		gobaresip.UseExternalBaresip(), // **Use an external Baresip process**
		gobaresip.SetLogger(loggerAdapter),
		gobaresip.SetPingInterval(6*time.Second),
	)
	if err != nil {
		loggerAdapter.Fatal(err)
	}

	// Run Baresip Serve() method.
	// This is meant to be similar to the http.Serve() method with the difference that
	// it takes an explicit context that can be used to cancel the Baresip instance.
	// In this example, we assume there is an external Baresip process launched
	// so Serve() won't start any background process.
	// The Baresip instance can be terminated at any time using the baresipCancel() function.
	// Communication happens using the event/response channels... keep reading
	baresipCtx, baresipCancel := context.WithCancel(context.Background())
	go func() {
		err := gb.Serve(baresipCtx)
		if err != nil {
			if errors.Is(err, gobaresip.ErrNoCtrlConn) {
				loggerAdapter.Error("Baresip control connection not established. Did you start in another terminal a 'baresip' instance with the ctrl_tcp module enabled?")
			} else {
				loggerAdapter.Errorf("baresip exit error: %s", err)
			}
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

				// your logic to handle responses goes here. Or you use [Baresip.CmdTxWithAck()] and
				// then you can avoid reading the response channel.
			}
		}
	}()

	go func() {
		// Give baresip some time to init and register SIP User Agent
		time.Sleep(5 * time.Second)

		// Dial a dummy phone number -- the [Baresip.Cmd] function requires you to read from the "response channel"
		if err := gb.Cmd("dial", "012345", "my_token"); err != nil {
			log.Println(err)
		}
	}()

	go func() {
		time.Sleep(15 * time.Second)

		// Terminate baresip instance after 15 seconds... this is just a demo, you would normally not do this.
		if err := gb.Cmd("quit", "", "quit_token"); err != nil {
			log.Println(err)
		}
	}()

	go func() {
		// stop this application after 20sec
		time.Sleep(20 * time.Second)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)
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
