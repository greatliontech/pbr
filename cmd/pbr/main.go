package main

import (
	"fmt"
	"log"
	"log/slog"

	"github.com/containers/storage/pkg/reexec"
	"github.com/containers/storage/pkg/unshare"
	"github.com/greatliontech/pbr/pkg/config"
	"github.com/greatliontech/pbr/pkg/registry"
)

func main() {
	if reexec.Init() {
		return
	}

	fmt.Println("Starting PBR")

	unshare.MaybeReexecUsingUserNamespace(false)

	c, err := config.FromFile("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	reg, err := registry.New(c)
	if err != nil {
		log.Fatal(err)
	}

	slog.Info("Listening on", "addr", c.Address, "host", c.Host)

	if err := reg.ServeH2C(); err != nil {
		log.Fatal(err)
	}
}
