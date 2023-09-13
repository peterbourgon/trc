package main

import (
	"context"
	"log"
	"time"
)

func contextSleep(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

//
//
//

type logWriter struct{ *log.Logger }

func (w *logWriter) Write(p []byte) (int, error) {
	w.Logger.Print(string(p))
	return len(p), nil
}
