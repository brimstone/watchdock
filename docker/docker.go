package docker

import (
	"encoding/json"
	//"github.com/davecgh/go-spew/spew"
	dockerclient "github.com/fsouza/go-dockerclient"
	"log"
)

type Processing struct {
	docker *dockerclient.Client
}

func (self *Processing) Init(socket string) error {
	var err error
	// Connect to our docker instance
	self.docker, err = dockerclient.NewClient(socket)
	if err != nil {
		return err
	}
	return nil
}

func (self *Processing) scanContainers(channel chan<- map[string]interface{}) error {
	// Get a list of what's currently running
	runningContainers, err := self.docker.ListContainers(dockerclient.ListContainersOptions{All: true})
	if err != nil {
		log.Fatal(err)
	}
	// Send all of the valid containers back to the storage module
	for _, c := range runningContainers {
		log.Println("Found already running container", c.Names[0])
		container, err := self.docker.InspectContainer(c.ID)
		rawContainer, err := json.Marshal(container)
		if err != nil {
			log.Fatal(err)
		}
		containerObj := new(map[string]interface{})
		err = json.Unmarshal(rawContainer, &containerObj)
		if err != nil {
			log.Fatal(err)
		}
		channel <- *containerObj
	}
	return nil
}

func (self *Processing) Sync(readChannel <-chan map[string]interface{}, writeChannel chan<- map[string]interface{}) {

	go self.scanContainers(writeChannel)

	for {
		select {
		case event := <-readChannel:
			log.Println("Docker got a message %v", event["Config"])
		}
	}
}

func New(socket string) (*Processing, error) {
	self := new(Processing)
	err := self.Init(socket)
	if err != nil {
		return nil, err
	}
	return self, nil
}
