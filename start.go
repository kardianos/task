package task

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"
)

// StartFunc is called during start-up, but should watch the
// context to check if it should shutdown.
type StartFunc func(ctx context.Context) error

// Start listens for an interrupt signal, and cancels the context if
// interrupted. It starts run in a new goroutine. If it takes more then
// stopTimeout before run returns after the ctx is canceled, then
// it returns regardless.
func Start(ctx context.Context, stopTimeout time.Duration, run StartFunc) error {
	notify := make(chan os.Signal, 3)
	signal.Notify(notify, os.Interrupt)
	ctx, cancel := context.WithCancel(ctx)
	once := &sync.Once{}
	fin := make(chan bool)
	unlock := func() {
		close(fin)
	}
	unlockOnce := func() {
		once.Do(unlock)
	}
	runErr := atomic.Value{}
	go func() {
		err := run(ctx)
		if err != nil {
			runErr.Store(err)
		}
		unlockOnce()
	}()
	select {
	case <-notify:
	case <-fin:
	}
	cancel()
	go func() {
		<-time.After(stopTimeout)
		unlockOnce()
	}()
	<-fin
	if err, ok := runErr.Load().(error); ok {
		return err
	}
	return nil
}
