package main

import (
	"log"
	"time"

	"github.com/f18m/go-baresip/pkg/gobaresip"
	"go.uber.org/zap"
)

// zapLogger is a concrete implementation of Logger using zap's SugaredLogger
type zapLogger struct {
	*zap.SugaredLogger
}

func main() {
	logger, _ := zap.NewProduction()
	loggerAdapter := zapLogger{logger.Sugar()}

	loggerAdapter.Info("Golang Baresip Example starting")

	gb, err := gobaresip.New(
		gobaresip.SetAudioPath("/usr/share/sounds"),
		// gobaresip.SetWsAddr("0.0.0.0:8080"),
		gobaresip.SetDebug(true),
		gobaresip.SetLogger(loggerAdapter),
	)
	if err != nil {
		loggerAdapter.Fatal(err)
	}

	cancelFunc, err := gb.Start()
	if err != nil {
		loggerAdapter.Errorf("failed starting baresip: %s", err)
	}

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
			case r, ok := <-rChan:
				if !ok {
					continue
				}
				log.Println("RESPONSE: " + string(r.RawJSON))
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

	err = gb.WaitForShutdown()
	if err != nil {
		loggerAdapter.Errorf("failed waiting baresip: %s", err)
	}

	cancelFunc()
	loggerAdapter.Info("Baresip has been stopped")
}
