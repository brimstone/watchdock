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
	"time"
)

var docker *dockerclient.DockerClient

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

// Callback used to listen to Docker's events
func eventCallback(event *dockerclient.Event, args ...interface{}) {
	switch event.Status {
	/*case "create":
	case "start":
		c, err := addContainer(event.Id)
		c.Register()
		if err != nil {
			log.Println("err:", err)
		}
	case "die":
		err := removeContainer(event.Id)
		if err != nil {
			log.Println("err:", err)
		}
	case "destroy":
	case "delete":
	*/
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

func main() {
	// Function level variables
	var err error

	// parse our cli flags
	var dockerSock = flag.String("docker", "unix:///var/run/docker.sock", "Path to docker socket")
	var otherConsul stringSlice
	flag.Var(&otherConsul, "join", "Clients to join")
	flag.Parse()

	// Init the docker client
	docker, err = dockerclient.NewDockerClient(*dockerSock, nil)
	if err != nil {
		log.Fatal(err)
	}

	// [todo] - otherConsul options need to be added, bootstrap-expect value needs to be updated
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
	consulStatus := consul.Status()

	// Find our leader so the user knows we've connected right
	leader, err := consulStatus.Leader()
	for err != nil {
		log.Println("Warning: ", err)
		time.Sleep(time.Second)
		leader, err = consulStatus.Leader()
	}
	for leader == "" && err == nil {

		log.Println("No leader and no error, waiting for a valid leader")
		leader, err = consulStatus.Leader()
		time.Sleep(2 * time.Second)
	}
	// let the users know we found the leader
	log.Println("Consul leader is", leader)

	/*
		log.Println("Finished enumerating containers, starting watch for docker events.")
		// Listen to events
		docker.StartMonitorEvents(eventCallback)
		// Periodically check on our services, forever
		time.Sleep(2 * time.Hour)
	*/
}
