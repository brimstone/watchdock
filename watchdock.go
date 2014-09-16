package main

import (
	"flag"
	"log"
	//"github.com/brimstone/watchdock/channel"
	stringslice "github.com/brimstone/go-stringslice"
	"github.com/brimstone/watchdock/dir"
	"time"
)

/* So here's the idea:

There's a number of storage modules, right now only DIR and CONSUL.

There's only one job running module, docker

Intialize our local container slice as empty

Ask the storage module once.

Ask the docker module once.

Tell the storage module it's now ok to run concurrently and handle events

Tell the docker module it's now ok to run concurrently and handle events

Sleep, forever

*/
type Package interface {
	Sync(chan map[string]interface{})
}

type Module struct {
	Channel chan map[string]interface{}
	Instance Package
}

func main() {
	otherConsul := new(stringslice.StringSlice)
	dirs := new(stringslice.StringSlice)
	// parse our command line args
	//var dockerSock = flag.String("docker", "unix:///var/run/docker.sock", "Path to docker socket")
	flag.Var(otherConsul, "join", "Clients to join")
	flag.Var(dirs, "dir", "Directories to store")
	flag.Parse()

	// todo - Use the following empty channel done := make(chan bool)

	var modules []Module
	for _, dirSeed := range []string(*dirs) {
		module := new(Module)
		var err error
		module.Channel = make(chan map[string]interface{})
		module.Instance, err = dir.New(dirSeed)
		if err != nil {
			log.Println("Error loading module dir")
		}
		modules = append(modules, *module)
	}

	// Start all of our modules
	for _, module := range modules {
		go module.Instance.Sync(module.Channel)
	}

	log.Println("Startup Finished")
	for {
		select {
		case container := <-containerChannel:
			log.Println("Main loop knows about", container["name"])
		case <-time.After(5 * time.Second):
			//	log.Println("Timeout in main loop")
		}
	}
	// todo - Use the following to wait forever: <-done
}
