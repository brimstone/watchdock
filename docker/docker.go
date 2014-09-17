package docker

import (
	"encoding/json"
	//"github.com/davecgh/go-spew/spew"
	"errors"
	dockerclient "github.com/fsouza/go-dockerclient"
	"log"
)

type Processing struct {
	docker     *dockerclient.Client
	containers map[string]bool
}

func (self *Processing) Init(socket string) error {
	var err error
	// Connect to our docker instance
	self.docker, err = dockerclient.NewClient(socket)
	if err != nil {
		return err
	}
	self.containers = make(map[string]bool)
	return nil
}

func (self *Processing) sendContainer(channel chan<- map[string]interface{}, container *dockerclient.Container) {
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
		if err != nil {
			log.Fatal(err)
		}
		self.sendContainer(channel, container)
	}
	return nil
}

func (self *Processing) listenToDocker(channel chan<- map[string]interface{}) {
	blah := make(chan *dockerclient.APIEvents, 10)
	self.docker.AddEventListener(blah)
	for {
		event := <-blah
		switch event.Status {
		case "start":
			container, err := self.docker.InspectContainer(event.ID)
			if err != nil {
				log.Fatal(err)
			}
			if !self.containers[container.Name] {
				self.containers[container.Name] = true
				self.sendContainer(channel, container)
			}
		case "destroy":
			// todo - Keep internal mapping of IDs to names
			// When a container is destroyed, all I'm going to know is the ID.
			// I need to lookup the name from the ID, and send an event with some special attribute.
			// This attribute will inform the storage module that it should forget what it knows about the container by this name.
			log.Println("Docker should send notification about this not existing")
		default:
			log.Println("Docker says", event.ID, event.Status)
		}
	}
}

func (self *Processing) Sync(readChannel <-chan map[string]interface{}, writeChannel chan<- map[string]interface{}) {

	go self.scanContainers(writeChannel)

	go self.listenToDocker(writeChannel)

	log.Println("Docker listening for events from storage module")
	for {
		select {
		case event := <-readChannel:
			log.Println("Docker got notification about", event["Name"])
			self.CheckOn(event)
		}
	}
}

func (self *Processing) findContainerByName(name string, running bool) (*dockerclient.Container, error) {
	runningContainers, err := self.docker.ListContainers(dockerclient.ListContainersOptions{All: !running})
	if err != nil {
		log.Fatal(err)
	}
	//spew.Dump(runningContainers)
	for _, c := range runningContainers {
		if len(c.Names) == 0 {
			continue
		}
		// If we find one
		if c.Names[0] == name {
			return self.docker.InspectContainer(c.ID)
		}
	}
	return nil, errors.New("Not found")
}

func (self *Processing) CheckOn(container map[string]interface{}) error {
	name := container["Name"].(string)
	_, err := self.findContainerByName(name, false)
	if err != nil {
		log.Println("Couldn't find container", name)
		return self.startContainer(container)
	}
	// todo - actually check the config
	log.Println("Container", name, "is already running")
	return nil
}

func (self *Processing) startContainer(container map[string]interface{}) error {
	log.Println("Starting container", container["Name"])
	rawJson, err := json.Marshal(container["Config"])
	config := new(dockerclient.Config)
	err = json.Unmarshal(rawJson, &config)
	name := container["Name"].(string)
	options := dockerclient.CreateContainerOptions{
		Name:   name,
		Config: config,
	}
	// remember this name for later
	self.containers[name] = true
	_, err = self.docker.CreateContainer(options)
	if err != nil {
		log.Println("Error starting container", err.Error())
		return err
	}
	return nil
}

func New(socket string) (*Processing, error) {
	self := new(Processing)
	err := self.Init(socket)
	if err != nil {
		return nil, err
	}
	return self, nil
}
