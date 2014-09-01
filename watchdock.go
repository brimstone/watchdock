// Dynamic Consul Node/service creation based on docker containers
package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/armon/consul-api"
	//"github.com/davecgh/go-spew/spew"
	dockerclient "github.com/fsouza/go-dockerclient"
	"log"
	"strconv"
	"strings"
	"time"
)

// Define our global variables
var docker *dockerclient.Client

var consul *consulapi.Client
var consulInstance *dockerclient.Container
var consulContainer Container
var otherConsul stringSlice

// Define a type named "stringSlice" as a slice of strings
type stringSlice []string

// Now, for our new type, implement the two methods of
// the flag.Value interface...
// The first method is String() string
func (i *stringSlice) String() string {
	return fmt.Sprint(*i)
}

// The second method is Set(value string) error
func (i *stringSlice) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func compareStringSlice(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		if b[i] != x {
			return false
		}
	}
	return true
}

type Container struct {
	Name         string
	Hostname     string
	Image        string
	MaxInstances int
	Pty          bool
	BindTo       string
	Cmd          []string
	Env          []string
	Ports        map[dockerclient.Port][]dockerclient.PortBinding
	Hosts        []string
	Volumes      map[string]struct{}
	VolumesFrom  []string
	Where        []string
}

var containers map[string]Container

/*
// Callback used to listen to Docker's events
func dockerCallback(event *dockerclient.Event, args ...interface{}) {
	log.Println("Detected docker", event.Status, "event")
	switch event.Status {
	case "die":
		if event.Id == consulInstance.Id {
			consulInstance, consul = findConsul(consulContainer)
		}
	case "destroy":
	case "delete":
	// [todo] - Make and call findContainerById(event.Id)
	default:
		log.Printf("Received event: %#v\n", *event)
	}
}
*/

func findContainerByName(name string, running bool) (*dockerclient.Container, error) {
	runningContainers, err := docker.ListContainers(dockerclient.ListContainersOptions{All: !running})
	if err != nil {
		log.Fatal(err)
	}
	//spew.Dump(runningContainers)
	for _, c := range runningContainers {
		if len(c.Names) == 0 {
			continue
		}
		// If we find one
		if c.Names[0] == "/"+name {
			return docker.InspectContainer(c.ID)
		}
	}
	return nil, errors.New("Not found")
}

func runContainer(name string, image string, tag string, createConfig *dockerclient.Config, hostConfig *dockerclient.HostConfig) (*dockerclient.Container, error) {
	log.Println("Creating a container from", image)
	// We didn't find a consul container, create one
	createConfig.Image = image
	_, err := docker.CreateContainer(dockerclient.CreateContainerOptions{Name: name, Config: createConfig})
	if err != nil {
		// if error is Not found, pull down the image and try creating the container again
		if err.Error() == "no such image" {
			log.Println("Container doesn't exist", image)
			log.Println("Pulling container", image)
			err = docker.PullImage(dockerclient.PullImageOptions{Repository: image, Tag: tag}, dockerclient.AuthConfiguration{})
			if err != nil {
				log.Printf("Error: runContainer 1: %s\n", err.Error())
				return nil, err
			}
			return runContainer(name, image, tag, createConfig, hostConfig)
		}
		log.Printf("Error: runContainer 2: %s\n", err.Error())
		return nil, err
	}
	consulContainer, err := findContainerByName("consul", false)
	if err != nil {
		log.Printf("Error: runContainer 3: %s\n", err.Error())
		return nil, err
	}
	err = docker.StartContainer(consulContainer.ID, hostConfig)
	if err != nil {
		log.Printf("Error: runContainer 4: %s\n", err.Error())
		return nil, err
	}
	return consulContainer, nil
}

func findConsul(consulContainer Container) (*dockerclient.Container, *consulapi.Client) {
	// Find our container
	consulInstance, err := startInstance(consulContainer)
	if err != nil {
		log.Fatal("findConsul: ", err)
	}
	// get its IP
	// [todo] - handle a blank ip address
	consulConfig := consulapi.DefaultConfig()
	consulConfig.Address = consulInstance.NetworkSettings.IPAddress + ":8500"
	log.Println("Found consul at", consulConfig.Address)

	// Establish our client
	consul, _ := consulapi.NewClient(consulConfig)
	return consulInstance, consul
}

func startInstance(container Container) (*dockerclient.Container, error) {
	// Look for an existing consul container
	log.Println("Looking for existing " + container.Name + " container")
	instance, err := findContainerByName(container.Name, false)

	// If we have a container, and the cmd line isn't the same, kill it and redefine err
	if err == nil && !compareStringSlice(instance.Config.Cmd, container.Cmd) {
		log.Println("Existing " + container.Name + " container was not started the way we expect.")
		log.Println("Making it new")
		docker.StopContainer(instance.ID, 0)
		docker.RemoveContainer(dockerclient.RemoveContainerOptions{ID: instance.ID})
		instance, err = findContainerByName(container.Name, false)
	}

	// if we didn't find our container
	if err != nil {
		if err.Error() != "Not found" {
			log.Printf("Error: startInstance 1: %s\n", err.Error())
			return nil, err
		}
		// figure out its cmd line
		createConfig := &dockerclient.Config{
			Hostname: container.Hostname,
			Cmd:      container.Cmd,
			Tty:      container.Pty,
		}
		hostConfig := &dockerclient.HostConfig{
			PortBindings: container.Ports,
		}
		// start our container
		// [todo] - need to support tags at some point, split on the :
		instance, err = runContainer(container.Name, container.Image, "latest", createConfig, hostConfig)
		if err != nil {
			log.Printf("Error: startInstance 2: %s\n", err.Error())
			return nil, err
		}
	}
	// [consider] - purge the old one, really shake the boat

	// Now we have a container
	// If it's not running
	if !instance.State.Running {
		// start it
		err = docker.StartContainer(instance.ID, nil)
		for instance.NetworkSettings.IPAddress == "" {
			log.Println("Waiting for " + container.Name + " container to get IP settings")
			instance, err = findContainerByName(container.Name, false)
			if err != nil {
				log.Printf("Error: startInstance 3: %s\n", err.Error())
				return nil, err
			}
			time.Sleep(time.Second)
		}
	}
	return instance, nil
}

func mapKVPairs() *map[string]Container {
	kv := consul.KV()

	pairs, _, _ := kv.List("containers", nil)

	containers := make(map[string]Container)
	// This will basically loop through every container item under in our pair
	for i := len(pairs) - 1; i >= 0; i-- {
		// Figure out how many levels are in our key
		levels := strings.Split(pairs[i].Key, "/")
		// we don't care about the top level keys
		if len(levels) < 3 || levels[2] == "" {
			continue
		}
		// extract our container, there's probably a better way
		container, _ := containers[levels[1]]
		container.Name = levels[1]
		// convert our KV value to a real string
		value := string(pairs[i].Value)
		// big switch that unmarshalls the KV into a Container
		switch levels[2] {
		case "image":
			container.Image = value
			//case "ports":
			//container.Ports = strings.Split(value, ",")
		case "hostname":
			container.Hostname = value
		case "maxinstances":
			container.MaxInstances, _ = strconv.Atoi(value)
		//case "volumes":
		//container.Volumes = strings.Split(value, ",")
		case "volumesfrom":
			container.VolumesFrom = strings.Split(value, ",")
		case "where":
			container.Where = strings.Split(value, ",")
		case "pty":
			container.Pty = value == "true"
		}
		// finally set the container back
		containers[levels[1]] = container
	}
	// return it to our calling function
	return &containers
}

func pullContainer(container Container) {
	log.Printf("Pulling %s\n", container.Image)
	err := docker.PullImage(dockerclient.PullImageOptions{Repository: container.Image, Tag: "latest"}, dockerclient.AuthConfiguration{})
	if err != nil {
		log.Printf("Error while pulling %s: %s\n", container.Image, err.Error())
	}
}

func pullContainers(containers map[string]Container) {
	for _, container := range containers {
		pullContainer(container)
	}
}

func startContainers(containers map[string]Container) {
	for name, container := range containers {
		log.Printf("Checking status of %s\n", name)
		startInstance(container)
	}
}

func cleanUntaggedContainers() {
	runningContainers, err := docker.ListContainers(dockerclient.ListContainersOptions{All: true})
	if err != nil {
		log.Fatal(err)
	}
	images, err := docker.ListImages(false)
	for _, c := range runningContainers {
		instance, _ := docker.InspectContainer(c.ID)
		for _, image := range images {
			if image.ID != instance.Image {
				continue
			}
			if image.RepoTags[0] == "<none>:<none>" {
				log.Printf("Cleaning up old container %s\n", instance.ID)
				docker.StopContainer(instance.ID, 0)
				docker.RemoveContainer(dockerclient.RemoveContainerOptions{ID: instance.ID})
			}
		}
	}
}

func cleanImages() {
	images, _ := docker.ListImages(false)
	for _, image := range images {
		if image.RepoTags[0] == "<none>:<none>" {
			log.Printf("Removing untagged image %s\n", image.ID)
			docker.RemoveImage(image.ID)
		}
	}
}

func waitForLeader() string {
	leader := ""
	var err error
	// While we don't have a leader
	for leader == "" {
		log.Println("Looking for consul leader")
		consulInstance, consul = findConsul(consulContainer)
		consulStatus := consul.Status()

		// Find our leader so the user knows we've connected right
		leader, err = consulStatus.Leader()
		log.Println("leader is", leader)
		// If we have an error getting the leader, wait a second, then try again
		startTime := time.Now()
		for err != nil && time.Since(startTime) < time.Minute {
			log.Println("Warning: ", err)
			time.Sleep(time.Second)
			leader, err = consulStatus.Leader()
		}
		// Remember when we started waiting for leader election to happen
		startTime = time.Now()
		// break if we get leader, an error, or it times out
		for leader == "" && err == nil && time.Since(startTime) < time.Minute {
			log.Println("No leader and no error, waiting for a valid leader")
			leader, err = consulStatus.Leader()
			time.Sleep(2 * time.Second)
			for _, x := range otherConsul {
				agent := consul.Agent()
				agent.Join(x, false)
			}
		}
		// If we still don't have a leader, than we timed out
		if leader == "" {
			log.Println("Timeout while waiting on leader election, killing the container")
			docker.StopContainer(consulInstance.ID, 0)
			docker.RemoveContainer(dockerclient.RemoveContainerOptions{ID: consulInstance.ID})
		}
	}

	// let the users know we found the leader
	log.Println("Consul leader is", leader)

	return leader
}

func main() {
	// Function level variables
	var err error

	// parse our cli flags
	var dockerSock = flag.String("docker", "unix:///var/run/docker.sock", "Path to docker socket")
	flag.Var(&otherConsul, "join", "Clients to join")
	flag.Parse()

	// Init the docker client
	docker, err = dockerclient.NewClient(*dockerSock)
	if err != nil {
		log.Fatal(err)
	}

	// Here's where we define our consul container
	// Build our consul cmd line from our options
	cmd := []string{
		"--bootstrap-expect", strconv.Itoa(len(otherConsul) + 1),
	}
	consulContainer.Cmd = cmd
	consulContainer.Name = "consul"
	consulContainer.Image = "brimstone/consul"
	consulContainer.Ports = make(map[dockerclient.Port][]dockerclient.PortBinding)
	consulContainer.Ports["8500/tcp"] = []dockerclient.PortBinding{
		dockerclient.PortBinding{
			HostIp:   "0.0.0.0",
			HostPort: "8500",
		},
	}
	consulContainer.Ports["8301/tcp"] = []dockerclient.PortBinding{
		dockerclient.PortBinding{
			HostIp:   "0.0.0.0",
			HostPort: "8301",
		},
	}
	consulContainer.Volumes = make(map[string]struct{})
	consulContainer.Env = make([]string, 0)

	log.Println("Finished enumerating containers, starting watch for docker events.")
	// Listen to events
	// [fixme] - docker.StartMonitorEvents(dockerCallback)
	// Periodically check on our services, forever
	for {
		// Wait for our leader
		waitForLeader()
		// Gather up all of the containers we should now about
		containers = *mapKVPairs()

		// pull down all of the images
		pullContainers(containers)
		pullContainer(consulContainer)
		pullContainer(Container{Image: "brimstone/watchdock"})

		// clean up dead containers
		cleanUntaggedContainers()
		// clean up untagged images
		cleanImages()
		// start what's not running
		startContainers(containers)
		// [todo] - clean up untagged images
		// sleep for a bit
		time.Sleep(2 * time.Minute)
		// make sure our consul container is running
		consulInstance, consul = findConsul(consulContainer)
		for _, x := range otherConsul {
			agent := consul.Agent()
			agent.Join(x, false)
		}
	}
}
