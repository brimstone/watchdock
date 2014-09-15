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

func main() {
	otherConsul := new(stringslice.StringSlice)
	// parse our command line args
	//var dockerSock = flag.String("docker", "unix:///var/run/docker.sock", "Path to docker socket")
	flag.Var(otherConsul, "join", "Clients to join")
	flag.Parse()

	//joins := []string(*otherConsul)

	containerChannel := make(chan map[string]interface{})
// todo - Use the following empty channel done := make(chan bool)

	module, err := dir.New("/tmp/containers")
	if err != nil {
		log.Println("Error loading module dir")
	}

	go module.Sync(containerChannel)

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
