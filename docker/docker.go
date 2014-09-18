package docker

import (
	"encoding/json"
	"errors"
	//"github.com/davecgh/go-spew/spew"
	dockerclient "github.com/fsouza/go-dockerclient"
	"log"
)

func logit(v ...interface{}) {
	log.Println("Docker:", v)
}

type Processing struct {
	docker     *dockerclient.Client
	containers []Container
}

type Container struct {
	ID   string
	Name string
}

func (self *Processing) findInternalContainerByName(name string) (*Container, error) {
	//spew.Dump(self.containers)
	for i, _ := range self.containers {
		c := &self.containers[i]
		if c.Name == name {
			return c, nil
		}
	}
	return nil, errors.New("container not found")
}

func (self *Processing) findInternalContainerByID(ID string) (*Container, error) {
	//spew.Dump(self.containers)
	for i, _ := range self.containers {
		c := &self.containers[i]
		if c.ID == ID {
			return c, nil
		}
	}
	return nil, errors.New("container not found")
}

func (self *Processing) Init(socket string) error {
	var err error
	// Connect to our docker instance
	self.docker, err = dockerclient.NewClient(socket)
	if err != nil {
		return err
	}
	//self.containers = new([]Container)
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
		logit("Found already running container", c.Names[0])
		container, err := self.docker.InspectContainer(c.ID)
		if err != nil {
			log.Fatal(err)
		}
		self.containers = append(self.containers, Container{Name: c.Names[0], ID: c.ID})
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
			if _, ok := self.findInternalContainerByName(container.Name); ok != nil {
				c := Container{Name: container.Name, ID: event.ID}
				self.containers = append(self.containers, c)
				self.sendContainer(channel, container)
			}
		case "destroy":
			// todo - Keep internal mapping of IDs to names
			// When a container is destroyed, all I'm going to know is the ID.
			// I need to lookup the name from the ID, and send an event with some special attribute.
			// This attribute will inform the storage module that it should forget what it knows about the container by this name.
			container, err := self.findInternalContainerByID(event.ID)
			if err != nil {
				continue
			}
			logit("Sending notification about this not existing")
			obj := make(map[string]interface{})
			obj["Name"] = container.Name
			obj["deleteme"] = true
			for i, c := range self.containers {
				if c.ID == event.ID {
					if i == 0 {
						self.containers = self.containers[1:]
					} else if i == len(self.containers) {
						self.containers = self.containers[0 : len(self.containers)-1]
					} else {
						self.containers = append(self.containers[:i], self.containers[i+1:]...)
					}
				}
			}
			channel <- obj

		default:
			logit("Docker says", event.ID, event.Status)
		}
	}
}

func (self *Processing) Sync(readChannel <-chan map[string]interface{}, writeChannel chan<- map[string]interface{}) {

	go self.scanContainers(writeChannel)

	go self.listenToDocker(writeChannel)

	logit("Listening for events from storage module")
	for {
		select {
		case event := <-readChannel:
			logit("Got notification about", event["Name"])
			logit(event)
			if _, ok := event["deleteme"]; ok {
				logit("Killing", event["Name"])
				container, err := self.findContainerByName("/"+event["Name"].(string), false)
				if err != nil {
					logit("Couldn't find container named", event["Name"])
					continue
				}
				self.docker.KillContainer(dockerclient.KillContainerOptions{ID: container.ID})
				continue
			}
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
		logit("Couldn't find container", name)
		return self.startContainer(container)
	}
	// todo - actually check the config
	logit("Container", name, "is already running")
	return nil
}

func (self *Processing) startContainer(container map[string]interface{}) error {
	logit("Starting container", container["Name"])
	// todo - handle pull first and all of what I've already figured out
	rawJson, err := json.Marshal(container["Config"])
	config := new(dockerclient.Config)
	err = json.Unmarshal(rawJson, &config)
	name := container["Name"].(string)
	options := dockerclient.CreateContainerOptions{
		Name:   name,
		Config: config,
	}
	// remember this name for later
	self.containers = append(self.containers, Container{Name: name})
	containerObj, err := self.docker.CreateContainer(options)
	if err != nil {
		logit("Error starting container", err.Error())
		return err
	}
	// todo - This doesn't pass back in a reference properly
	c, _ := self.findInternalContainerByName(name)
	c.ID = containerObj.ID
	//spew.Dump(self.containers)
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
