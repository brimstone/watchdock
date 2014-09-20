package docker

import (
	"encoding/json"
	"errors"
	//"github.com/davecgh/go-spew/spew"
	dockerclient "github.com/fsouza/go-dockerclient"
	"log"
	"strings"
	"time"
)

func logit(v ...interface{}) {
	log.Println("Docker:", v)
}

type Processing struct {
	docker     *dockerclient.Client
	containers []Container
	Images     map[string]string
}

type Container struct {
	ID         string
	Name       string
	Image      string
	Protect    bool
	Config     *dockerclient.Config
	HostConfig *dockerclient.HostConfig
}

func (self *Processing) findInternalContainerByName(name string) (*Container, error) {
	for i, _ := range self.containers {
		c := &self.containers[i]
		if c.Name == name {
			return c, nil
		}
	}
	return nil, errors.New("container not found")
}

func (self *Processing) findInternalContainerByID(ID string) (*Container, error) {
	for i, _ := range self.containers {
		c := &self.containers[i]
		if c.ID == ID {
			return c, nil
		}
	}
	return nil, errors.New("container not found")
}

func (self *Processing) appendContainer(container Container) {
	var c *Container
	var err error
	c, err = self.findInternalContainerByName(container.Name)
	if err != nil {
		logit("Error", err.Error())
	}
	if c != nil {
		if container.ID != "" {
			c.ID = container.ID
		}
		c.Config = container.Config
		c.HostConfig = container.HostConfig
		logit("Found container already!", c.Name)
	} else {
		logit("Couldn't find existing container to update, adding a new one")
		for _, c := range self.containers {
			logit("Container", c.ID, c.Name, c.Image)
		}
		logit("New container", container.Name, container.Image)
		self.containers = append(self.containers, container)
		if _, ok := self.Images[container.Image]; !ok {
			self.Images[container.Image] = "fresh"
		}
	}
}

func (self *Processing) Init(socket string) error {
	var err error
	// Connect to our docker instance
	self.docker, err = dockerclient.NewClient(socket)
	if err != nil {
		return err
	}
	//self.containers = new([]Container)
	self.Images = make(map[string]string)
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
		fullContainer, err := self.docker.InspectContainer(c.ID)
		if err != nil {
			log.Fatal(err)
		}
		container := Container{
			Name:       c.Names[0],
			ID:         c.ID,
			Image:      c.Image,
			Config:     fullContainer.Config,
			HostConfig: fullContainer.HostConfig,
		}
		self.appendContainer(container)
		self.sendContainer(channel, fullContainer)
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
				c := Container{
					Name:    container.Name,
					ID:      event.ID,
					Image:   container.Image,
					Protect: false,
				}
				self.containers = append(self.containers, c)
				self.sendContainer(channel, container)
			}
		case "destroy":
			// When a container is destroyed, all I'm going to know is the ID.
			// I need to lookup the name from the ID, and send an event with some special attribute.
			// This attribute will inform the storage module that it should forget what it knows about the container by this name.
			container, err := self.findInternalContainerByID(event.ID)
			if err != nil {
				continue
			}
			if container.Protect {
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
			rawConfig, err := json.Marshal(event["Config"])
			config := new(dockerclient.Config)
			err = json.Unmarshal(rawConfig, &config)
			if err != nil {
				logit("Error, bad json passed to us")
				continue
			}
			rawHostConfig, err := json.Marshal(event["HostConfig"])
			hostConfig := new(dockerclient.HostConfig)
			err = json.Unmarshal(rawHostConfig, &hostConfig)
			//spew.Dump(hostConfig)
			if err != nil {
				logit("Error, bad json passed to us")
				continue
			}

			c := Container{
				Name:       event["Name"].(string),
				Config:     config,
				HostConfig: hostConfig,
				Image:      config.Image,
			}
			self.appendContainer(c)
			go self.CheckOn(c)
		case <-time.After(10 * time.Second):
			self.pullAllImages()
			self.CheckOnContainers()
			self.removeUntaggedImages()
			self.removeUntaggedContainers()
			//spew.Dump(self.containers)
		}
	}
}

func (self *Processing) CheckOnContainers() {
	// todo - this needs to check on any containers,
	// start them if they're stopped
	// unset protection flag
	for i, c := range self.containers {
		self.CheckOn(c)
		self.containers[i].Protect = true
	}
}

func (self *Processing) removeUntaggedImages() {
	for i, pulling := range self.Images {
		if pulling == "pulling" {
			logit("Currently pulling", i, "so not removing images")
			return
		}
	}
	images, _ := self.docker.ListImages(false)
	for _, image := range images {
		if image.RepoTags[0] == "<none>:<none>" {
			logit("Removing untagged image", image.ID)
			self.docker.RemoveImage(image.ID)
		}
	}
}

func (self *Processing) removeUntaggedContainers() {
	runningContainers, err := self.docker.ListContainers(dockerclient.ListContainersOptions{All: true})
	if err != nil {
		logit(err)
		return
	}
	images, err := self.docker.ListImages(false)
	for _, c := range runningContainers {
		instance, _ := self.docker.InspectContainer(c.ID)
		for _, image := range images {
			if image.ID != instance.Image {
				continue
			}
			if image.RepoTags[0] == "<none>:<none>" {
				c, _ := self.findInternalContainerByID(c.ID)
				c.Protect = true
				logit("Cleaning up old container", instance.ID)
				self.docker.StopContainer(instance.ID, 0)
				self.docker.RemoveContainer(dockerclient.RemoveContainerOptions{ID: instance.ID})
			}
		}
	}
}

func (self *Processing) findContainerByName(name string, running bool) (*dockerclient.Container, error) {
	runningContainers, err := self.docker.ListContainers(dockerclient.ListContainersOptions{All: !running})
	if err != nil {
		log.Fatal(err)
	}
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

func (self *Processing) CheckOn(container Container) error {
	name := container.Name
	c, err := self.findContainerByName(name, false)
	if err != nil {
		logit("Couldn't find container", name)
		return self.startContainer(container)
	}
	if c.State.Running {
		// todo - actually check the config
		logit("Container", name, "is already running")
		return nil
	}
	logit("Container", name, "is not running, need to start it")
	//spew.Dump(container)
	return nil
}

func (self *Processing) pullImage(imageName string) error {
	if imageName == "" {
		log.Fatal("I can't pull nothing. You've got something wrong")
	}
	image := strings.Split(imageName, ":")
	if len(image) == 1 {
		image = append(image, "latest")
	}
	if self.Images[image[0]] == "pulling" {
		return errors.New("Already pulling " + imageName)
	}
	logit("Pulling", imageName)
	self.Images[image[0]] = "pulling"
	err := self.docker.PullImage(dockerclient.PullImageOptions{Repository: image[0], Tag: image[1]}, dockerclient.AuthConfiguration{})
	self.Images[image[0]] = "idle"
	if err != nil {
		return err
	}
	return nil
}

func (self *Processing) pullAllImages() {
	logit("Pulling all Images")
	// Make a temp channel
	channel := make(chan struct{})
	for image, _ := range self.Images {
		// run all of our pulls concurrently
		go func() {
			self.pullImage(image)
			logit("Image", image, "finished pulling")
			// notify our parent when we're done
			channel <- struct{}{}
		}()
	}
	// wait for all of the images to complete their pull
	for _, _ = range self.Images {
		<-channel
	}
	logit("Finished checking for new images")
}

func (self *Processing) startContainer(container Container) error {
	logit("Starting container", container.Name)
	/*
		if container.Image == "" {
				spew.Dump(container)
			}
	*/
	err := self.pullImage(container.Image)
	if err != nil {
		logit("Error pulling", container.Name, err.Error())
	}
	options := dockerclient.CreateContainerOptions{
		Name:   container.Name,
		Config: container.Config,
	}
	// remember this name for later
	containerObj, err := self.docker.CreateContainer(options)
	if err != nil {
		logit("Error starting container", err.Error())
		return err
	}
	c, _ := self.findInternalContainerByName(container.Name)
	c.ID = containerObj.ID
	err = self.docker.StartContainer(c.ID, container.HostConfig)
	if err != nil {
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
