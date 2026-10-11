package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/router"
)

func main() {
	err := config.Setup()
	if err != nil {
		panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = router.InitializeContext(ctx)
	if err != nil {
		panic(err)
	}

}
