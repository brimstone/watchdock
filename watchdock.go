package main

import (
	"flag"
	"log"
	//"github.com/brimstone/watchdock/channel"
	"github.com/brimstone/watchdock/consul"
	"github.com/brimstone/watchdock/dir"
	"github.com/brimstone/watchdock/docker"
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
type Module interface {
	Sync(<-chan map[string]interface{}, chan<- map[string]interface{})
}

func main() {
	// parse our command line args
	var dockerSock = flag.String("docker", "unix:///var/run/docker.sock", "Path to docker socket")
	consulSeed := flag.String("consul", "", "Connection information for consul")
	dirSeed := flag.String("dir", "", "Directory to store")
	flag.Parse()

	done := make(chan bool)

	storageChannel := make(chan map[string]interface{})
	processingChannel := make(chan map[string]interface{})

	var storageModule Module
	if *dirSeed != "" {
		var err error
		storageModule, err = dir.New(*dirSeed)
		if err != nil {
			log.Println("Error loading module dir")
		} else {
			log.Println("Loaded storage module: dir")
		}
	}
	// todo - add consul check here
	if *consulSeed != "" {
		var err error
		storageModule, err = consul.New(*consulSeed)
		if err != nil {
			log.Println("Error loading module consul")
		} else {
			log.Println("Loaded storage module: consul")
		}
	}

	if storageModule == nil {
		log.Fatal("No storage module loaded successfully")
	}

	processingModule, err := docker.New(*dockerSock)
	if err != nil {
		log.Fatal("Error loading module docker")
	}

	// Start all of our modules

	go storageModule.Sync(storageChannel, processingChannel)
	go processingModule.Sync(processingChannel, storageChannel)

	log.Println("Startup Finished")
	<-done

}
