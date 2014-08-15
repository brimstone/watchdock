// Dynamic Consul Node/service creation based on docker containers
package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/armon/consul-api"
	//"github.com/davecgh/go-spew/spew"
	"github.com/samalba/dockerclient"
	"log"
	"strconv"
	"strings"
	"time"
)

// Define our global variables
var docker *dockerclient.DockerClient

var otherConsul stringSlice
var consul *consulapi.Client
var consulContainer *dockerclient.ContainerInfo

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
	Volumes      []string
	Ports        []string
	Hosts        []string
	Where        []string
}

var containers map[string]Container

// Callback used to listen to Docker's events
func dockerCallback(event *dockerclient.Event, args ...interface{}) {
	log.Println("Detected docker", event.Status, "event")
	switch event.Status {
	/*case "create":
	case "start":
		c, err := addContainer(event.Id)
		c.Register()
		if err != nil {
			log.Println("err:", err)
		}
	*/
	case "die":
		if event.Id == consulContainer.Id {
			consulContainer, consul = findConsul()
		}
	case "destroy":
	case "delete":
	default:
		log.Printf("Received event: %#v\n", *event)
	}
}

func findContainer(name string, running bool) (*dockerclient.ContainerInfo, error) {
	runningContainers, err := docker.ListContainers(!running)
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range runningContainers {
		// If we find one
		if c.Names[0] == "/consul" {
			return docker.InspectContainer(c.Id)
		}
	}
	return nil, errors.New("Not found")
}

func runContainer(name string, image string, tag string, config *dockerclient.ContainerConfig) (*dockerclient.ContainerInfo, error) {
	log.Println("Creating a container", image)
	// We didn't find a consul container, create one
	config.Image = image
	_, err := docker.CreateContainer(config, name)
	if err != nil {
		// if error is Not found, pull down the image and try creating the container again
		if err.Error() == "Not found" {
			log.Println("Container doesn't exist", image)
			log.Println("Pulling container", image)
			err = docker.PullImage(image, tag)
			if err != nil {
				return nil, err
			}
			return runContainer(name, image, tag, config)
		}
		return nil, err
	}
	consulContainer, err := findContainer("/consul", false)
	if err != nil {
		return nil, err
	}
	err = docker.StartContainer(consulContainer.Id, nil)
	if err != nil {
		return nil, err
	}
	return consulContainer, nil
}

func findConsul() (*dockerclient.ContainerInfo, *consulapi.Client) {
	// Build our consul cmd line from our options
	cmd := []string{
		"--bootstrap-expect", strconv.Itoa(len(otherConsul) + 1),
	}
	for _, x := range otherConsul {
		cmd = append(cmd, "--join")
		cmd = append(cmd, x)
	}

	// Look for an existing consul container
	log.Println("Looking for existing consul container")
	consulContainer, err := findContainer("/consul", false)

	// If we have a container, and the cmd line isn't the same, kill it and redefine err
	if err == nil && !compareStringSlice(consulContainer.Config.Cmd, cmd) {
		log.Println("Existing consul container was not started the way we expect.")
		log.Println("Making it new")
		docker.StopContainer(consulContainer.Id, 0)
		docker.RemoveContainer(consulContainer.Id)
		consulContainer, err = findContainer("/consul", false)
	}

	// if we didn't find  our container
	if err != nil {
		if err.Error() != "Not found" {
			log.Fatal(err)
		}
		// figure out its cmd line
		config := &dockerclient.ContainerConfig{
			Cmd: cmd,
		}
		// start our container
		consulContainer, err = runContainer("consul", "brimstone/consul", "latest", config)
		if err != nil {
			log.Fatal(err)
		}
	}
	// [consider] - purge the old one, really shake the boat

	// Now we have a container
	// If it's not running
	if !consulContainer.State.Running {
		// start it
		err = docker.StartContainer(consulContainer.Id, nil)
		for consulContainer.NetworkSettings.IpAddress == "" {
			log.Println("Waiting for consul container to get IP settings")
			consulContainer, err = findContainer("/consul", false)
			if err != nil {
				log.Fatal(err)
			}
			time.Sleep(time.Second)
		}
	}
	// get its IP
	// [todo] - handle a blank ip address
	consulConfig := consulapi.DefaultConfig()
	consulConfig.Address = consulContainer.NetworkSettings.IpAddress + ":8500"
	log.Println("Found consul at", consulConfig.Address)

	// Establish our client
	consul, _ := consulapi.NewClient(consulConfig)
	return consulContainer, consul
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
		// convert our KV value to a real string
		value := string(pairs[i].Value)
		// big switch that unmarshalls the KV into a Container
		switch levels[2] {
		case "image":
			container.Image = value
		case "ports":
			container.Ports = strings.Split(value, ",")
		case "hostname":
			container.Hostname = value
		case "maxinstances":
			container.MaxInstances, _ = strconv.Atoi(value)
		case "volumes":
			container.Volumes = strings.Split(value, ",")
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

func main() {
	// Function level variables
	var err error

	// parse our cli flags
	var dockerSock = flag.String("docker", "unix:///var/run/docker.sock", "Path to docker socket")
	flag.Var(&otherConsul, "join", "Clients to join")
	flag.Parse()

	// Init the docker client
	docker, err = dockerclient.NewDockerClient(*dockerSock, nil)
	if err != nil {
		log.Fatal(err)
	}

	leader := ""
	// While we don't have a leader
	for leader == "" {
		log.Println("Looking for consul leader")
		consulContainer, consul = findConsul()
		consulStatus := consul.Status()

		// Find our leader so the user knows we've connected right
		leader, err = consulStatus.Leader()
		log.Println("leader is", leader)
		// If we have an error getting the leader, wait a second, then try again
		for err != nil {
			log.Println("Warning: ", err)
			time.Sleep(time.Second)
			leader, err = consulStatus.Leader()
		}
		// Remember when we started waiting for leader election to happen
		startTime := time.Now()
		// break if we get leader, an error, or it times out
		for leader == "" && err == nil && time.Since(startTime) < time.Minute {
			log.Println("No leader and no error, waiting for a valid leader")
			leader, err = consulStatus.Leader()
			time.Sleep(2 * time.Second)
		}
		// If we still don't have a leader, than we timed out
		if leader == "" {
			log.Println("Timeout while waiting on leader election, killing the container")
			docker.StopContainer(consulContainer.Id, 0)
			docker.RemoveContainer(consulContainer.Id)
		}
	}

	// let the users know we found the leader
	log.Println("Consul leader is", leader)

	log.Println("Finished enumerating containers, starting watch for docker events.")
	// Listen to events
	docker.StartMonitorEvents(dockerCallback)
	// Periodically check on our services, forever
	for {
		containers = *mapKVPairs()
		time.Sleep(5 * time.Second)
		consulContainer, consul = findConsul()
	}
}
