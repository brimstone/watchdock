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

func (self *Processing) Sync(channel chan map[string]interface{}) {
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

	for {
		select {
		case event := <-channel:
			log.Println("Docker got a message %v", event)
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
